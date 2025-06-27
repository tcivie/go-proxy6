package pipeline

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

// HTTPPayload represents an HTTP request flowing through the pipeline
type HTTPPayload struct {
	ID           string
	Ctx          context.Context
	Request      *http.Request
	Response     http.ResponseWriter
	BindAddr     *net.TCPAddr
	Target       string
	done         chan error
	complete     int32 // atomic flag
	responseSent int32 // atomic flag
	mu           sync.Mutex
}

// NewHTTPPayload creates a new HTTP payload for pipeline processing
func NewHTTPPayload(id string, ctx context.Context, req *http.Request, resp http.ResponseWriter) *HTTPPayload {
	return &HTTPPayload{
		ID:       id,
		Ctx:      ctx,
		Request:  req,
		Response: resp,
		done:     make(chan error, 1),
	}
}

// Clone creates a deep copy of the payload
func (p *HTTPPayload) Clone() Payload {
	p.mu.Lock()
	defer p.mu.Unlock()

	return &HTTPPayload{
		ID:           p.ID,
		Ctx:          p.Ctx,
		Request:      p.Request,
		Response:     p.Response,
		BindAddr:     p.BindAddr,
		Target:       p.Target,
		done:         p.done,
		complete:     atomic.LoadInt32(&p.complete),
		responseSent: atomic.LoadInt32(&p.responseSent),
	}
}

// MarkAsProcessed is called when payload processing is complete
func (p *HTTPPayload) MarkAsProcessed() {
	// Ensure completion if not already done
	if !p.IsComplete() {
		p.Complete(nil)
	}
}

// Done returns the completion channel
func (p *HTTPPayload) Done() chan error {
	return p.done
}

// Complete marks the payload as complete with optional error
func (p *HTTPPayload) Complete(err error) {
	if atomic.CompareAndSwapInt32(&p.complete, 0, 1) {
		select {
		case p.done <- err:
		default:
		}
	}
}

// IsComplete returns true if the payload has been completed
func (p *HTTPPayload) IsComplete() bool {
	return atomic.LoadInt32(&p.complete) == 1
}

// MarkResponseSent marks that a response has been sent to the client
func (p *HTTPPayload) MarkResponseSent() {
	atomic.StoreInt32(&p.responseSent, 1)
}

// ResponseSent returns true if a response has been sent to the client
func (p *HTTPPayload) ResponseSent() bool {
	return atomic.LoadInt32(&p.responseSent) == 1
}
