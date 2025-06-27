package pipeline

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
)

// RequestData represents the structure for request data
type RequestData struct {
	HTTPReq  *http.Request
	HTTPResp http.ResponseWriter
	Target   string
	BindAddr *net.TCPAddr
}

// Request represents data flowing through the pipeline
type Request struct {
	ID   string
	Data *RequestData
	Ctx  context.Context
	Done chan error
}

// Stage is the interface that all pipeline stages must implement
//
//go:generate mockgen -destination=mocks/mock_stage.go -package=mocks go-proxy6/internal/pipeline Stage
type Stage interface {
	Process(req *Request) error
	Name() string
}

// Pipeline processes requests through a series of stages with a single worker
type Pipeline struct {
	stages []Stage
	input  chan *Request
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new pipeline with the given stages (single worker for optimal performance)
func New(stages ...Stage) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pipeline{
		stages: stages,
		input:  make(chan *Request, 10),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins processing requests through the pipeline with a single worker
func (p *Pipeline) Start() {
	slog.Debug("Starting pipeline with single worker")

	p.wg.Add(1)
	go p.worker()

	slog.Debug("Pipeline started with single worker")
}

// Process sends a request through the pipeline
func (p *Pipeline) Process(req *Request) {
	select {
	case p.input <- req:
		slog.Debug("Request enqueued", slog.String("request_id", req.ID))
	case <-p.ctx.Done():
		slog.Debug("Pipeline context canceled", slog.String("request_id", req.ID))
		req.Done <- p.ctx.Err()
	}
}

// Stop gracefully shuts down the pipeline
func (p *Pipeline) Stop() {
	slog.Debug("Stopping pipeline")
	p.cancel()
	close(p.input)
	p.wg.Wait()
	slog.Debug("Pipeline stopped")
}

// worker processes requests through all stages (single worker implementation)
func (p *Pipeline) worker() {
	defer func() {
		p.wg.Done()
		slog.Debug("Worker stopped")
	}()

	slog.Debug("Worker started")

	for {
		select {
		case <-p.ctx.Done():
			return
		case req, ok := <-p.input:
			if !ok {
				return
			}

			slog.Debug("Processing request", slog.String("request_id", req.ID))

			var err error
			for _, stage := range p.stages {
				slog.Debug("Entering stage", slog.String("stage", stage.Name()), slog.String("request_id", req.ID))
				if err = stage.Process(req); err != nil {
					slog.Debug("Stage failed", slog.String("stage", stage.Name()), slog.String("request_id", req.ID), slog.String("error", err.Error()))
					break
				}
				slog.Debug("Stage completed", slog.String("stage", stage.Name()), slog.String("request_id", req.ID))
			}

			select {
			case req.Done <- err:
			case <-p.ctx.Done():
				return
			}

			if err != nil {
				slog.Debug("Request processed with error", slog.String("request_id", req.ID), slog.String("error", err.Error()))
			} else {
				slog.Debug("Request processed successfully", slog.String("request_id", req.ID))
			}
		}
	}
}
