package pipeline

import (
	"context"
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
type Stage interface {
	Process(req *Request) error
	Name() string
}

// Pipeline processes requests through a series of stages
type Pipeline struct {
	stages  []Stage
	input   chan *Request
	workers int
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a new pipeline with the given stages
func New(workers int, stages ...Stage) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pipeline{
		stages:  stages,
		input:   make(chan *Request, workers),
		workers: workers,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins processing requests through the pipeline
func (p *Pipeline) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Process sends a request through the pipeline
func (p *Pipeline) Process(req *Request) {
	select {
	case p.input <- req:
	case <-p.ctx.Done():
		req.Done <- p.ctx.Err()
	}
}

// Stop gracefully shuts down the pipeline
func (p *Pipeline) Stop() {
	p.cancel()
	close(p.input)
	p.wg.Wait()
}

// worker processes requests through all stages
func (p *Pipeline) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case req, ok := <-p.input:
			if !ok {
				return
			}

			// Process through all stages
			var err error
			for _, stage := range p.stages {
				if err = stage.Process(req); err != nil {
					break
				}
			}

			// Send result
			select {
			case req.Done <- err:
			case <-p.ctx.Done():
				return
			}
		}
	}
}
