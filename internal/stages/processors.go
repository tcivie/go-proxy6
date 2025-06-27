package stages

import (
	"context"
	"crypto/tls"
	"fmt"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"io"
	"log"
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
		if !httpPayload.ResponseSent() {
			http.Error(httpPayload.Response, "IPv6 address generation failed", http.StatusInternalServerError)
			httpPayload.MarkResponseSent()
		}
		httpPayload.Complete(fmt.Errorf("IPv6 generation failed: %v", err))
		return nil, nil
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
			if !httpPayload.ResponseSent() {
				http.Error(httpPayload.Response, "Missing host", http.StatusBadRequest)
				httpPayload.MarkResponseSent()
			}
			httpPayload.Complete(fmt.Errorf("missing host"))
			return nil, nil
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
	return nil, nil
}

// Simple CONNECT handler that creates an IPv6 tunnel
func (p *ProxyProcessor) handleConnect(payload *pipeline.HTTPPayload) error {
	target := payload.Target
	bindAddr := payload.BindAddr
	resp := payload.Response

	log.Printf("CONNECT request to: %s via IPv6: %s", target, bindAddr.IP.String())

	// Connect to target using our IPv6 address
	dialer := &net.Dialer{
		LocalAddr: bindAddr,
		Timeout:   10 * time.Second,
	}

	// Direct connection to target through IPv6
	targetConn, err := dialer.Dial("tcp6", target)
	if err != nil {
		if !payload.ResponseSent() {
			http.Error(resp, "Failed to connect to target", http.StatusBadGateway)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to connect to %s: %v", target, err)
	}
	defer targetConn.Close()

	// Log successful connection
	log.Printf("Successfully connected to %s", target)

	// Hijack client connection
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

	// Send 200 Connection Established
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	if err != nil {
		return fmt.Errorf("failed to send 200 response: %v", err)
	}
	payload.MarkResponseSent()

	// Set reasonable timeouts
	clientConn.SetDeadline(time.Now().Add(5 * time.Minute))
	targetConn.SetDeadline(time.Now().Add(5 * time.Minute))

	// Create bidirectional tunnel
	errCh := make(chan error, 2)

	// Copy from client to target
	go func() {
		_, err := io.Copy(targetConn, clientConn)
		errCh <- err
	}()

	// Copy from target to client
	go func() {
		_, err := io.Copy(clientConn, targetConn)
		errCh <- err
	}()

	// Wait for either copy to finish
	err = <-errCh
	if err != nil && err != io.EOF {
		return fmt.Errorf("tunnel error: %v", err)
	}

	return nil
}

// Handle HTTP requests
func (p *ProxyProcessor) handleHTTP(payload *pipeline.HTTPPayload) error {
	req := payload.Request
	resp := payload.Response
	bindAddr := payload.BindAddr

	// Determine if this is HTTPS
	isHTTPS := req.TLS != nil || req.Header.Get("X-Forwarded-Proto") == "https" ||
		strings.HasSuffix(payload.Target, ":443")

	// Create a simple transport with IPv6 dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Force IPv6-only connections using our bind address
			dialer := &net.Dialer{
				LocalAddr: bindAddr,
				Timeout:   10 * time.Second,
			}
			return dialer.Dial("tcp6", addr)
		},
		// Basic TLS config
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		// Standard timeouts
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     30 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Limit the number of redirects
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil // Allow redirects
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

	// Copy headers
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
			http.Error(resp, "Proxy request failed", http.StatusBadGateway)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("proxy request failed: %v", err)
	}
	defer proxyResp.Body.Close()

	// Copy response headers
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

// Simple IPv6 dialer that enforces IPv6-only connections
func (p *ProxyProcessor) dialWithIPv6(addr string, localIP net.IP) (net.Conn, error) {
	// Parse target address
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %s: %v", addr, err)
	}

	// Resolve all IPs (IPv4 and IPv6)
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %v", host, err)
	}

	// Find IPv6 addresses
	var ipv6s []net.IP
	for _, ip := range ips {
		if ip.To4() == nil {
			ipv6s = append(ipv6s, ip)
		}
	}

	if len(ipv6s) == 0 {
		return nil, fmt.Errorf("no IPv6 addresses found for %s", host)
	}

	// Parse port
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("invalid port %s: %v", port, err)
	}

	// Create bind address
	bindAddr := &net.TCPAddr{
		IP: localIP,
	}

	// Try each IPv6 address
	var lastErr error
	for _, ip := range ipv6s {
		targetAddr := &net.TCPAddr{
			IP:   ip,
			Port: portNum,
		}

		dialer := &net.Dialer{
			LocalAddr: bindAddr,
			Timeout:   10 * time.Second,
		}

		conn, err := dialer.Dial("tcp6", targetAddr.String())
		if err != nil {
			lastErr = err
			continue
		}

		log.Printf("Connected to %s (%s) via IPv6", host, ip.String())
		return conn, nil
	}

	return nil, fmt.Errorf("failed to connect to any IPv6 address for %s: %v", host, lastErr)
}

// isHopByHopHeader checks if a header should not be forwarded
func isHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailers", "transfer-encoding", "upgrade":
		return true
	}
	return false
}
