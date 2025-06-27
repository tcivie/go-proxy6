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
	"sync"
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

	// Log the CONNECT request
	log.Printf("CONNECT request to: %s via IPv6: %s", target, bindAddr.IP.String())

	// First, hijack the client connection
	hijacker, ok := resp.(http.Hijacker)
	if !ok {
		if !payload.ResponseSent() {
			http.Error(resp, "Hijacking not supported", http.StatusInternalServerError)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("hijacking not supported")
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		if !payload.ResponseSent() {
			http.Error(resp, "Failed to hijack connection", http.StatusInternalServerError)
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to hijack connection: %v", err)
	}
	defer clientConn.Close()

	// Create context with timeout for connection
	connCtx, connCancel := context.WithTimeout(payload.Ctx, 30*time.Second)
	defer connCancel()

	// Connect to the target with IPv6 binding
	dialer := &net.Dialer{
		LocalAddr: bindAddr,
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Parse the target address for logging
	host, port, _ := net.SplitHostPort(target)
	log.Printf("Establishing CONNECT tunnel to %s:%s", host, port)

	// Connect to the target
	destConn, err := dialer.DialContext(connCtx, "tcp6", target)
	if err != nil {
		// Send error response if we haven't already
		if !payload.ResponseSent() {
			clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
			payload.MarkResponseSent()
		}
		return fmt.Errorf("failed to connect to target %s: %v", target, err)
	}
	defer destConn.Close()

	// Set reasonable buffer sizes
	if tcpConn, ok := destConn.(*net.TCPConn); ok {
		tcpConn.SetReadBuffer(65536)
		tcpConn.SetWriteBuffer(65536)
		tcpConn.SetNoDelay(true)
	}

	// Mark the response as sent to prevent Go from sending another response
	payload.MarkResponseSent()

	// Send the 200 Connection Established response
	// This is the MOST CRITICAL part - it must be exactly in this format
	success := "HTTP/1.1 200 Connection established\r\n\r\n"
	if _, err := clientConn.Write([]byte(success)); err != nil {
		return fmt.Errorf("failed to send 200 response: %v", err)
	}

	log.Printf("CONNECT tunnel established to %s", target)

	// Flush any buffered data from hijacked connection to the target
	if clientBuf.Reader.Buffered() > 0 {
		buf := make([]byte, clientBuf.Reader.Buffered())
		clientBuf.Reader.Read(buf)
		destConn.Write(buf)
	}

	// Use a WaitGroup to manage our goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy from client to target
	go func() {
		defer wg.Done()
		buf := make([]byte, 65536)
		for {
			// Set a read deadline
			clientConn.SetReadDeadline(time.Now().Add(5 * time.Minute))

			n, err := clientConn.Read(buf)
			if n > 0 {
				// Set a write deadline
				destConn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if _, err := destConn.Write(buf[:n]); err != nil {
					log.Printf("Error writing to target: %v", err)
					break
				}
			}
			if err != nil {
				if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
					log.Printf("Error reading from client: %v", err)
				}
				break
			}
		}

		// Signal the server we're done writing
		if tcpConn, ok := destConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		} else {
			destConn.Close()
		}
	}()

	// Copy from target to client
	go func() {
		defer wg.Done()
		buf := make([]byte, 65536)
		for {
			// Set a read deadline
			destConn.SetReadDeadline(time.Now().Add(5 * time.Minute))

			n, err := destConn.Read(buf)
			if n > 0 {
				// Set a write deadline
				clientConn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if _, err := clientConn.Write(buf[:n]); err != nil {
					log.Printf("Error writing to client: %v", err)
					break
				}
			}
			if err != nil {
				if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
					log.Printf("Error reading from target: %v", err)
				}
				break
			}
		}

		// Signal the client we're done writing
		if tcpConn, ok := clientConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		} else {
			clientConn.Close()
		}
	}()

	// Wait for copying to complete or context to be canceled
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("CONNECT tunnel closed normally for %s", target)
		return nil
	case <-payload.Ctx.Done():
		log.Printf("CONNECT tunnel closed due to context cancellation for %s", target)
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

	// Resolve target IP (force IPv6)
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 10 * time.Second,
				LocalAddr: &net.UDPAddr{
					IP: bindAddr.IP,
				},
			}
			// Force IPv6 DNS resolution
			return d.DialContext(ctx, "udp6", address)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Resolve IPv6 addresses only
	ips, err := resolver.LookupIP(ctx, "ip6", host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s (IPv6): %v", host, err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPv6 addresses found for %s", host)
	}

	// Get port number
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("invalid port %s: %v", port, err)
	}

	// Try each IP until one works
	var lastErr error
	for _, ip := range ips {
		targetAddr := &net.TCPAddr{
			IP:   ip,
			Port: portNum,
		}

		dialer := &net.Dialer{
			LocalAddr: bindAddr,
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   nil, // No special socket options
		}

		// Connect directly to the IPv6 address
		conn, err := dialer.Dial("tcp6", targetAddr.String())
		if err != nil {
			lastErr = err
			log.Printf("Failed to connect to %s (%s): %v, trying next IP...",
				host, ip.String(), err)
			continue
		}

		// Set TCP options for better performance
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(30 * time.Second)
			// Larger buffers for better performance
			tcpConn.SetReadBuffer(128 * 1024)  // 128KB
			tcpConn.SetWriteBuffer(128 * 1024) // 128KB
		}

		log.Printf("Successfully connected to %s (%s)", host, ip.String())
		return conn, nil
	}

	return nil, fmt.Errorf("failed to connect to any IPv6 address for %s: %v", host, lastErr)
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
