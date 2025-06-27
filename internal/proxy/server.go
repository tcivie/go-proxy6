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
	ctx        context.Context
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

	// Start request processor
	s.wg.Add(1)
	go s.processRequests()

	// Setup HTTP handlers
	http.HandleFunc("/", s.handleRequest)

	log.Printf("Starting high-performance IPv6 proxy on %s", s.config.BindAddr)
	log.Printf("IPv6 subnet: %s", s.config.IPv6Subnet)
	log.Printf("Max workers: %d", s.maxWorkers)

	return http.ListenAndServe(s.config.BindAddr, nil)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Create request context with timeout
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	// Create payload
	payload := pipeline.NewHTTPPayload(
		generateID(),
		ctx,
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
				log.Printf("Request failed: %v", err)
				// Error response already handled by processor
			}
		case <-ctx.Done():
			http.Error(w, "Request timeout", http.StatusGatewayTimeout)
		}
	default:
		// Channel full, reject request
		http.Error(w, "Server overloaded", http.StatusServiceUnavailable)
	}
}

func (s *Server) processRequests() {
	defer s.wg.Done()

	// Create simple source that reads from channel
	source := &channelSource{ch: s.requestCh, ctx: s.ctx}

	// Create simple sink (requests complete themselves)
	sink := &noopSink{}

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			err := s.pipeline.Process(s.ctx, source, sink)
			if err != nil && s.ctx.Err() == nil {
				log.Printf("Pipeline error: %v", err)
				time.Sleep(100 * time.Millisecond) // Brief pause on error
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
	ch      chan *pipeline.HTTPPayload
	ctx     context.Context
	current *pipeline.HTTPPayload
	err     error
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
	case <-s.ctx.Done():
		s.err = s.ctx.Err()
		return false
	}
}

func (s *channelSource) Payload() pipeline.Payload {
	return s.current
}

func (s *channelSource) Error() error {
	return s.err
}

// noopSink implements pipeline.Sink but does nothing (requests complete themselves)
type noopSink struct{}

func (s *noopSink) Consume(ctx context.Context, payload pipeline.Payload) error {
	// Requests handle their own completion
	return nil
}

func generateID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
