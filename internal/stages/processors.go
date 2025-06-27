package stages

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
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
		// CONNECT method for HTTPS tunneling
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

	// Enhanced connection to target with IPv6 binding
	destConn, err := p.dialWithBind(target, bindAddr)
	if err != nil {
		if !payload.ResponseSent() {
			// Send proper HTTP error response for CONNECT failures
			resp.WriteHeader(http.StatusBadGateway)
			resp.Write([]byte("502 Bad Gateway - Failed to connect to target"))
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to connect to %s: %v", target, err)
	}
	defer destConn.Close()

	// Hijack the client connection
	hijacker, ok := resp.(http.Hijacker)
	if !ok {
		if !payload.ResponseSent() {
			resp.WriteHeader(http.StatusInternalServerError)
			resp.Write([]byte("500 Internal Server Error - Hijacking not supported"))
			payload.MarkResponseSent()
		}
		return fmt.Errorf("hijacking not supported")
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		if !payload.ResponseSent() {
			resp.WriteHeader(http.StatusInternalServerError)
			resp.Write([]byte("500 Internal Server Error - Failed to hijack connection"))
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to hijack connection: %v", err)
	}
	defer clientConn.Close()

	// Send proper 200 Connection established response
	connectResponse := "HTTP/1.1 200 Connection established\r\n\r\n"
	_, err = clientConn.Write([]byte(connectResponse))
	if err != nil {
		return fmt.Errorf("failed to send CONNECT response: %v", err)
	}
	payload.MarkResponseSent()

	// Set deadlines to prevent hangs
	clientConn.SetDeadline(time.Now().Add(5 * time.Minute))
	destConn.SetDeadline(time.Now().Add(5 * time.Minute))

	// Bidirectional copy with proper error handling and larger buffers
	errChan := make(chan error, 2)

	// Copy from client to destination with larger buffer
	go func() {
		buf := make([]byte, 65536)
		_, err := io.CopyBuffer(destConn, clientConn, buf)
		errChan <- err
	}()

	// Copy from destination to client with larger buffer
	go func() {
		buf := make([]byte, 65536)
		_, err := io.CopyBuffer(clientConn, destConn, buf)
		errChan <- err
	}()

	// Wait for first error, context cancellation, or deadline
	select {
	case err := <-errChan:
		// Ignore EOF errors which are normal when connections close
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
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
			// Force IPv6-only to prevent IP leakage
			return p.dialWithBind(addr, bindAddr)
		},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
			MaxVersion:         tls.VersionTLS13,
			// Add full suite of cipher suites for maximum compatibility
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				// Add additional cipher suites for compatibility
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			},
			// Add client-initiated renegotiation for problematic servers
			Renegotiation: tls.RenegotiateOnceAsClient,
		},
		// Increase all timeouts for better reliability
		DisableKeepAlives:     false,
		DisableCompression:    false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second, // Increased from 10s
		ResponseHeaderTimeout: 30 * time.Second, // Add timeout for headers
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
	// IPv6-only to prevent IP leakage
	if bindAddr.IP.To4() != nil {
		return nil, fmt.Errorf("IPv4 bind address not allowed - IPv6 only")
	}

	// Parse target address
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %s: %v", addr, err)
	}

	// Look up host (IPv6 only)
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %v", host, err)
	}

	// Find an IPv6 address
	var targetIP net.IP
	for _, ip := range ips {
		if ip.To4() == nil {
			targetIP = ip
			break
		}
	}

	if targetIP == nil {
		return nil, fmt.Errorf("no IPv6 address found for %s", host)
	}

	// Create target address
	targetAddr := &net.TCPAddr{
		IP:   targetIP,
		Port: parseInt(port),
	}

	// Special handling for known problematic sites
	var connectTimeout time.Duration = 15 * time.Second
	if strings.Contains(host, "ipv6-test.com") ||
		strings.Contains(host, "test-ipv6.com") {
		// These sites often need more time or special handling
		connectTimeout = 25 * time.Second
	}

	// Enhanced dialer with extended timeouts
	dialer := &net.Dialer{
		LocalAddr: bindAddr,
		Timeout:   connectTimeout,
		KeepAlive: 30 * time.Second,
		DualStack: false, // Force IPv6 only
	}

	// Connect directly to the IPv6 address
	conn, err := dialer.Dial("tcp6", targetAddr.String())
	if err != nil {
		// Provide detailed error info for debugging
		return nil, fmt.Errorf("IPv6 connection to %s (%s) failed: %v",
			host, targetIP, err)
	}

	// Configure TCP options for better performance
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)

		// Add larger buffers for better performance
		tcpConn.SetReadBuffer(65536)
		tcpConn.SetWriteBuffer(65536)
	}

	return conn, nil
}

// Helper function to parse port
func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
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
