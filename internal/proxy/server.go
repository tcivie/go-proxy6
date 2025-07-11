package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"go-proxy6/internal/config"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"go-proxy6/internal/stages"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type Server struct {
	config     *config.Config
	pipeline   *pipeline.Pipeline
	requestCh  chan *pipeline.HTTPPayload
	ctx        context.Context // Server context - for shutdown only
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	maxWorkers int
}

func NewServer(cfg *config.Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	// Default to 2x CPU cores for maximum performance
	maxWorkers := runtime.NumCPU() * 6
	if maxWorkers < 8 {
		maxWorkers = 8
	}

	return &Server{
		config:     cfg,
		requestCh:  make(chan *pipeline.HTTPPayload, 10_000),
		ctx:        ctx,
		cancel:     cancel,
		maxWorkers: maxWorkers,
	}
}

func (s *Server) SetMaxWorkers(workers int) {
	if workers > 0 {
		s.maxWorkers = workers
	}
}

func (s *Server) Start() error {
	// Create IPv6 generator
	gen := ipv6.NewGenerator(s.config.IPv6Net, s.config.IPv6Base)

	// Create pipeline with dynamic worker pools for maximum speed
	s.pipeline = pipeline.New(
		pipeline.DynamicWorkerPool(stages.NewIPv6Processor(gen), s.maxWorkers),
		pipeline.DynamicWorkerPool(stages.NewTargetProcessor(), s.maxWorkers),
		pipeline.DynamicWorkerPool(stages.NewProxyProcessor(), s.maxWorkers*2),
	)

	// Start request processor with proper error handling
	s.wg.Add(1)
	go s.processRequests()

	// Setup HTTP handlers
	server := &http.Server{
		Addr: s.config.BindAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.handleRequest(w, r)
		}),
	}

	slog.Info("Starting high-performance IPv6 proxy",
		"bind_addr", s.config.BindAddr,
		"ipv6_subnet", s.config.IPv6Subnet,
		"max_workers", s.maxWorkers,
	)

	return server.ListenAndServe()
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Create request-specific context with timeout (separate from server context)
	requestCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create payload with request-specific context
	id := generateID()
	payload := pipeline.NewHTTPPayload(
		id,
		requestCtx,
		r,
		w,
	)
	slog.Info("Request received",
		"id", id,
		"method", r.Method,
		"host", r.Host,
	)

	// Send to processing channel (non-blocking)
	select {
	case s.requestCh <- payload:
		// Wait for completion
		select {
		case err := <-payload.Done():
			if err != nil {
				slog.Error("Request failed",
					"request_id", payload.ID,
					"error", err)
				// Error response should already be handled by processor
				// If not handled, send a generic error
				if !payload.ResponseSent() {
					http.Error(w, "Proxy error", http.StatusBadGateway)
				}
			}
		case <-requestCtx.Done():
			if !payload.ResponseSent() {
				http.Error(w, "Request timeout", http.StatusGatewayTimeout)
			}
		case <-s.ctx.Done():
			// Server shutting down
			if !payload.ResponseSent() {
				http.Error(w, "Server shutting down", http.StatusServiceUnavailable)
			}
		}
	default:
		// Channel full, reject request
		http.Error(w, "Server overloaded", http.StatusServiceUnavailable)
	}
}

func (s *Server) processRequests() {
	defer s.wg.Done()
	source := &channelSource{ch: s.requestCh, serverCtx: s.ctx}
	sink := &errorHandlingSink{}

	// Keep processing requests until server shutdown
	for {
		select {
		case <-s.ctx.Done():
			slog.Info("Request processor shutting down...")
			return
		default:
			err := s.pipeline.Process(s.ctx, source, sink)
			if err != nil && s.ctx.Err() == nil {
				slog.Warn("Pipeline batch completed with errors (continuing)",
					"error", err)

				// Brief pause to prevent tight loop on persistent errors
				select {
				case <-time.After(100 * time.Millisecond):
				case <-s.ctx.Done():
					return
				}
			}
		}
	}
}

func (s *Server) Stop() {
	slog.Info("Shutting down proxy server...")
	s.cancel()
	close(s.requestCh)
	s.wg.Wait()
	slog.Info("Proxy server stopped")
}

// channelSource implements pipeline.Source for channel-based input
type channelSource struct {
	ch        chan *pipeline.HTTPPayload
	serverCtx context.Context
	current   *pipeline.HTTPPayload
	err       error
}

func (s *channelSource) Next(ctx context.Context) bool {
	select {
	case payload, ok := <-s.ch:
		if !ok {
			return false
		}
		s.current = payload
		return true
	case <-ctx.Done():
		s.err = ctx.Err()
		return false
	case <-s.serverCtx.Done():
		s.err = s.serverCtx.Err()
		return false
	}
}

func (s *channelSource) Payload() pipeline.Payload {
	return s.current
}

func (s *channelSource) Error() error {
	return s.err
}

// errorHandlingSink implements pipeline.Sink with proper error handling
type errorHandlingSink struct{}

func (s *errorHandlingSink) Consume(ctx context.Context, payload pipeline.Payload) error {
	// Requests handle their own completion, but we should never return errors
	// that would kill the pipeline. All request errors should be handled
	// at the request level.
	httpPayload := payload.(*pipeline.HTTPPayload)

	// Ensure the request is marked as complete even if processors failed
	if !httpPayload.IsComplete() {
		httpPayload.Complete(nil)
	}

	return nil // Never return errors that would kill the pipeline
}

func generateID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
