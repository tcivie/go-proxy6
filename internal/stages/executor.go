package stages

import (
	"context"
	"fmt"
	"go-proxy6/internal/pipeline"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
)

// RequestExecutorStage executes the actual proxy requests
type RequestExecutorStage struct{}

func NewRequestExecutor() *RequestExecutorStage {
	return &RequestExecutorStage{}
}

func (s *RequestExecutorStage) Name() string {
	return "RequestExecutor"
}

func (s *RequestExecutorStage) Process(req *pipeline.Request) error {
	slog.Debug("RequestExecutorStage processing",
		slog.String("stage", s.Name()),
		slog.String("request_id", req.ID))

	httpReq := req.Data.HTTPReq

	if httpReq.Method == http.MethodConnect {
		return s.handleConnect(req)
	}
	return s.handleHTTP(req)
}

func (s *RequestExecutorStage) handleConnect(req *pipeline.Request) error {
	target := req.Data.Target
	bindAddr := req.Data.BindAddr
	httpResp := req.Data.HTTPResp

	// Connect to target
	destConn, err := s.dialWithBind(target, bindAddr)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer destConn.Close()

	// Hijack connection
	hijacker, ok := httpResp.(http.Hijacker)
	if !ok {
		return fmt.Errorf("hijacking not supported")
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		return fmt.Errorf("failed to hijack: %v", err)
	}
	defer clientConn.Close()

	// Send 200 OK
	clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))

	// Pipe data
	errChan := make(chan error, 2)
	go s.copyData(destConn, clientConn, errChan)
	go s.copyData(clientConn, destConn, errChan)

	select {
	case <-req.Ctx.Done():
		return req.Ctx.Err()
	case err := <-errChan:
		return err
	}
}

func (s *RequestExecutorStage) handleHTTP(req *pipeline.Request) error {
	httpReq := req.Data.HTTPReq
	httpResp := req.Data.HTTPResp
	bindAddr := req.Data.BindAddr

	// Create client with custom dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return s.dialWithBind(addr, bindAddr)
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Build target URL
	targetURL := &url.URL{
		Scheme:   "http",
		Host:     httpReq.Host,
		Path:     httpReq.URL.Path,
		RawQuery: httpReq.URL.RawQuery,
	}

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(req.Ctx, httpReq.Method, targetURL.String(), httpReq.Body)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Copy headers
	for name, values := range httpReq.Header {
		for _, value := range values {
			proxyReq.Header.Add(name, value)
		}
	}

	// Execute request
	resp, err := client.Do(proxyReq)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Copy response headers
	for name, values := range resp.Header {
		for _, value := range values {
			httpResp.Header().Add(name, value)
		}
	}

	// Copy status and body
	httpResp.WriteHeader(resp.StatusCode)
	_, err = io.Copy(httpResp, resp.Body)
	return err
}

func (s *RequestExecutorStage) dialWithBind(addr string, bindAddr *net.TCPAddr) (net.Conn, error) {
	dialer := &net.Dialer{
		LocalAddr: bindAddr,
		Timeout:   10 * time.Second,
	}
	return dialer.Dial("tcp", addr)
}

func (s *RequestExecutorStage) copyData(dst, src net.Conn, errChan chan error) {
	_, err := io.Copy(dst, src)
	errChan <- err
}
