package stages

import (
	"context"
	"crypto/tls"
	"fmt"
	"go-proxy6/internal/pipeline"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

	slog.Info("CONNECT",
		"id", payload.ID,
		"target", target,
		"via", bindAddr.IP.String(),
	)

	// Hijack the connection FIRST, before any other operations
	hijacker, ok := resp.(http.Hijacker)
	if !ok {
		http.Error(resp, "Hijacking not supported", http.StatusInternalServerError)
		payload.MarkResponseSent()
		return fmt.Errorf("hijack not supported")
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		// If hijacking fails, we can still use http.Error since hijack hasn't succeeded
		http.Error(resp, "Failed to hijack", http.StatusInternalServerError)
		payload.MarkResponseSent()
		return err
	}

	// From this point on, we must handle all responses manually since we've hijacked
	defer clientConn.Close()
	payload.MarkResponseSent() // Mark immediately after hijack

	// Now attempt to connect to target
	dialer := &net.Dialer{
		LocalAddr: bindAddr,
		Timeout:   10 * time.Second,
	}
	targetConn, err := dialer.Dial("tcp6", target)
	if err != nil {
		// Send error response manually since we've hijacked
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\nFailed to connect to target\r\n"))
		return err
	}
	defer targetConn.Close()

	// Send 200 OK
	clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))

	// Proxy both ways
	go io.Copy(targetConn, clientConn)
	io.Copy(clientConn, targetConn)

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
	slog.Info("HTTP request",
		"id", payload.ID,
		"method", req.Method,
		"target_url", targetURL.String(),
		"via", bindAddr.IP.String(),
	)

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
	slog.Info("HTTP response",
		"id", payload.ID,
		"status_code", proxyResp.StatusCode,
	)
	return err
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
