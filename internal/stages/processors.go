package stages

import (
	"context"
	"crypto/tls"
	"fmt"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// IPv6Processor generates random IPv6 addresses
type IPv6Processor struct {
	generator *ipv6.Generator
}

func NewIPv6Processor(gen *ipv6.Generator) *IPv6Processor {
	return &IPv6Processor{generator: gen}
}

func (p *IPv6Processor) Process(ctx context.Context, payload pipeline.Payload) (pipeline.Payload, error) {
	httpPayload := payload.(*pipeline.HTTPPayload)

	addr, err := p.generator.RandomAddr()
	if err != nil {
		// Send error response and complete the request
		if !httpPayload.ResponseSent() {
			http.Error(httpPayload.Response, "IPv6 address generation failed", http.StatusInternalServerError)
			httpPayload.MarkResponseSent()
		}
		httpPayload.Complete(fmt.Errorf("IPv6 generation failed: %v", err))
		return nil, nil // Don't propagate error, request is handled
	}

	httpPayload.BindAddr = addr
	return httpPayload, nil
}

// TargetProcessor resolves target addresses and determines HTTPS
type TargetProcessor struct{}

func NewTargetProcessor() *TargetProcessor {
	return &TargetProcessor{}
}

func (p *TargetProcessor) Process(ctx context.Context, payload pipeline.Payload) (pipeline.Payload, error) {
	httpPayload := payload.(*pipeline.HTTPPayload)
	req := httpPayload.Request

	var target string
	if req.Method == http.MethodConnect {
		target = req.Host
		if !strings.Contains(target, ":") {
			target += ":443" // HTTPS default
		}
	} else {
		host := req.Host
		if host == "" {
			host = req.URL.Host
		}
		if host == "" {
			// Send error response and complete the request
			if !httpPayload.ResponseSent() {
				http.Error(httpPayload.Response, "Missing host", http.StatusBadRequest)
				httpPayload.MarkResponseSent()
			}
			httpPayload.Complete(fmt.Errorf("missing host"))
			return nil, nil // Don't propagate error, request is handled
		}

		// Determine port based on scheme or default
		if !strings.Contains(host, ":") {
			if req.TLS != nil || req.Header.Get("X-Forwarded-Proto") == "https" {
				host += ":443"
			} else {
				host += ":80"
			}
		}
		target = host
	}

	httpPayload.Target = target
	return httpPayload, nil
}

// ProxyProcessor executes HTTP/HTTPS proxy requests
type ProxyProcessor struct{}

func NewProxyProcessor() *ProxyProcessor {
	return &ProxyProcessor{}
}

func (p *ProxyProcessor) Process(ctx context.Context, payload pipeline.Payload) (pipeline.Payload, error) {
	httpPayload := payload.(*pipeline.HTTPPayload)

	var err error
	if httpPayload.Request.Method == http.MethodConnect {
		err = p.handleConnect(httpPayload)
	} else {
		err = p.handleHTTP(httpPayload)
	}

	// Always complete the request, regardless of error
	httpPayload.Complete(err)
	return nil, nil // Never propagate errors that would kill the pipeline
}

func (p *ProxyProcessor) handleConnect(payload *pipeline.HTTPPayload) error {
	target := payload.Target
	bindAddr := payload.BindAddr
	resp := payload.Response

	// Connect to target with binding
	destConn, err := p.dialWithBind(target, bindAddr)
	if err != nil {
		if !payload.ResponseSent() {
			http.Error(resp, fmt.Sprintf("Failed to connect to %s", target), http.StatusBadGateway)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to connect to %s: %v", target, err)
	}
	defer destConn.Close()

	// Hijack the client connection
	hijacker, ok := resp.(http.Hijacker)
	if !ok {
		if !payload.ResponseSent() {
			http.Error(resp, "Hijacking not supported", http.StatusInternalServerError)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("hijacking not supported")
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		if !payload.ResponseSent() {
			http.Error(resp, "Failed to hijack connection", http.StatusInternalServerError)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to hijack connection: %v", err)
	}
	defer clientConn.Close()

	// Send connection established response
	clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	payload.MarkResponseSent()

	// Bidirectional copy
	errChan := make(chan error, 2)
	go p.copyData(destConn, clientConn, errChan)
	go p.copyData(clientConn, destConn, errChan)

	// Wait for first error or context cancellation
	select {
	case err := <-errChan:
		return err
	case <-payload.Ctx.Done():
		return payload.Ctx.Err()
	}
}

func (p *ProxyProcessor) handleHTTP(payload *pipeline.HTTPPayload) error {
	req := payload.Request
	resp := payload.Response
	bindAddr := payload.BindAddr

	// Determine if this is HTTPS
	isHTTPS := req.TLS != nil || req.Header.Get("X-Forwarded-Proto") == "https" ||
		strings.HasSuffix(payload.Target, ":443")

	// Create transport with IPv6-only custom dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// SECURITY: Force IPv6-only to prevent IP leakage
			return p.dialWithBind(addr, bindAddr)
		},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		DisableKeepAlives:     false,
		DisableCompression:    false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	// Build target URL
	scheme := "http"
	if isHTTPS {
		scheme = "https"
	}

	targetURL := &url.URL{
		Scheme:   scheme,
		Host:     req.Host,
		Path:     req.URL.Path,
		RawQuery: req.URL.RawQuery,
		Fragment: req.URL.Fragment,
	}

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(payload.Ctx, req.Method, targetURL.String(), req.Body)
	if err != nil {
		if !payload.ResponseSent() {
			http.Error(resp, "Failed to create proxy request", http.StatusInternalServerError)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to create proxy request: %v", err)
	}

	// Copy headers (excluding hop-by-hop headers)
	for name, values := range req.Header {
		if !isHopByHopHeader(name) {
			for _, value := range values {
				proxyReq.Header.Add(name, value)
			}
		}
	}

	// Execute request
	proxyResp, err := client.Do(proxyReq)
	if err != nil {
		if !payload.ResponseSent() {
			// Provide more specific error messages based on the type of error
			var errorMsg string
			var statusCode int

			if strings.Contains(err.Error(), "no such host") {
				errorMsg = "Host not found"
				statusCode = http.StatusNotFound
			} else if strings.Contains(err.Error(), "connection refused") {
				errorMsg = "Connection refused"
				statusCode = http.StatusBadGateway
			} else if strings.Contains(err.Error(), "timeout") {
				errorMsg = "Connection timeout"
				statusCode = http.StatusGatewayTimeout
			} else {
				errorMsg = "Proxy request failed"
				statusCode = http.StatusBadGateway
			}

			http.Error(resp, errorMsg, statusCode)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("proxy request failed: %v", err)
	}
	defer proxyResp.Body.Close()

	// Copy response headers (excluding hop-by-hop headers)
	for name, values := range proxyResp.Header {
		if !isHopByHopHeader(name) {
			for _, value := range values {
				resp.Header().Add(name, value)
			}
		}
	}

	// Copy status code and body
	resp.WriteHeader(proxyResp.StatusCode)
	payload.MarkResponseSent()

	_, err = io.Copy(resp, proxyResp.Body)
	return err
}

func (p *ProxyProcessor) dialWithBind(addr string, bindAddr *net.TCPAddr) (net.Conn, error) {
	// SECURITY: IPv6-only to prevent IP leakage
	if bindAddr.IP.To4() != nil {
		return nil, fmt.Errorf("IPv4 bind address not allowed - IPv6 only")
	}

	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{
			IP: bindAddr.IP,
		},
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Force IPv6-only connection
	conn, err := dialer.Dial("tcp6", addr)
	if err != nil {
		// More detailed logging for debugging
		fmt.Printf("IPv6 dial failed - Request: %s, Local: %s, Target: %s, Error: %v\n",
			"", bindAddr.IP.String(), addr, err)
		return nil, fmt.Errorf("IPv6 connection failed: %v", err)
	}

	return conn, nil
}

func (p *ProxyProcessor) copyData(dst, src net.Conn, errChan chan error) {
	_, err := io.Copy(dst, src)
	errChan <- err
}

// isHopByHopHeader reports whether hdr is an RFC 2616 hop-by-hop header
func isHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate",
		"proxy-authorization", "te", "trailers", "transfer-encoding",
		"upgrade":
		return true
	}
	return false
}
