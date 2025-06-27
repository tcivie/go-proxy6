package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"go-proxy6/internal/config"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"go-proxy6/internal/stages"
	"net/http"
	"time"
)

type Server struct {
	config   *config.Config
	pipeline *pipeline.Pipeline
}

func NewServer(cfg *config.Config) *Server {
	// Create IPv6 generator
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)

	// Create a pipeline with single worker for optimal performance
	pipe := pipeline.New(
		stages.NewLogger(),           // Log all requests
		stages.NewIPv6Generator(gen), // Generate random IPv6
		stages.NewTargetResolver(),   // Resolve target address
		stages.NewRequestExecutor(),  // Execute the proxy request
	)

	return &Server{
		config:   cfg,
		pipeline: pipe,
	}
}

func (s *Server) Start() error {
	s.pipeline.Start()
	http.HandleFunc("/", s.handleRequest)
	return http.ListenAndServe(s.config.BindAddr, nil)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Create request context
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create pipeline request
	req := &pipeline.Request{
		ID:   generateID(),
		Ctx:  ctx,
		Done: make(chan error, 1),
		Data: &pipeline.RequestData{
			HTTPReq:  r,
			HTTPResp: w,
		},
	}

	// Process through pipeline
	s.pipeline.Process(req)

	// Wait for result
	select {
	case err := <-req.Done:
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	case <-ctx.Done():
		http.Error(w, "Timeout", http.StatusGatewayTimeout)
	}
}

func (s *Server) Stop() {
	s.pipeline.Stop()
}

func generateID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
