package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"go-proxy6/internal/config"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"go-proxy6/internal/stages"
	"log"
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
	maxWorkers := runtime.NumCPU() * 2
	if maxWorkers < 4 {
		maxWorkers = 4
	}

	return &Server{
		config:     cfg,
		requestCh:  make(chan *pipeline.HTTPPayload, 1000), // Buffered channel for speed
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
		pipeline.DynamicWorkerPool(stages.NewProxyProcessor(), s.maxWorkers/2), // Fewer workers for actual requests
	)

	// Start request processor with proper error handling
	s.wg.Add(1)
	go s.processRequests()

	// Setup HTTP handlers
	server := &http.Server{
		Addr: s.config.BindAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("Incoming request: %s %s -> %s", r.Method, r.Host, r.RequestURI)
			s.handleRequest(w, r)
		}),
	}

	log.Printf("Starting high-performance IPv6 proxy on %s", s.config.BindAddr)
	log.Printf("IPv6 subnet: %s", s.config.IPv6Subnet)
	log.Printf("Max workers: %d", s.maxWorkers)

	return server.ListenAndServe()
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Create request-specific context with timeout (separate from server context)
	requestCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create payload with request-specific context
	payload := pipeline.NewHTTPPayload(
		generateID(),
		requestCtx,
		r,
		w,
	)

	// Send to processing channel (non-blocking)
	select {
	case s.requestCh <- payload:
		// Wait for completion
		select {
		case err := <-payload.Done():
			if err != nil {
				log.Printf("Request %s failed: %v", payload.ID, err)
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

	// Create source that reads from channel
	source := &channelSource{ch: s.requestCh, serverCtx: s.ctx}

	// Create sink that handles errors gracefully
	sink := &errorHandlingSink{}

	// Keep processing requests until server shutdown
	for {
		select {
		case <-s.ctx.Done():
			log.Println("Request processor shutting down...")
			return
		default:
			// Process a batch of requests
			// Use a fresh context for each batch so errors don't kill the pipeline
			batchCtx, batchCancel := context.WithCancel(s.ctx)

			err := s.pipeline.Process(batchCtx, source, sink)
			batchCancel()

			if err != nil && s.ctx.Err() == nil {
				// Log the error but don't stop processing
				log.Printf("Pipeline batch completed with errors (continuing): %v", err)
			}

			// Brief pause to prevent tight loop on persistent errors
			select {
			case <-time.After(10 * time.Millisecond):
			case <-s.ctx.Done():
				return
			}
		}
	}
}

func (s *Server) Stop() {
	log.Println("Shutting down proxy server...")
	s.cancel()
	close(s.requestCh)
	s.wg.Wait()
	log.Println("Proxy server stopped")
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
