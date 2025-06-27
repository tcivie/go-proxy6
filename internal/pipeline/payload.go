package pipeline

import (
	"context"
	"net"
	"net/http"
)

// HTTPPayload represents an HTTP request flowing through the pipeline
type HTTPPayload struct {
	ID       string
	Ctx      context.Context
	Request  *http.Request
	Response http.ResponseWriter
	BindAddr *net.TCPAddr
	Target   string
	done     chan error
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
	return &HTTPPayload{
		ID:       p.ID,
		Ctx:      p.Ctx,
		Request:  p.Request,
		Response: p.Response,
		BindAddr: p.BindAddr,
		Target:   p.Target,
		done:     p.done,
	}
}

// MarkAsProcessed is called when payload processing is complete
func (p *HTTPPayload) MarkAsProcessed() {
	// Payload processing complete
}

// Done returns the completion channel
func (p *HTTPPayload) Done() chan error {
	return p.done
}

// Complete marks the payload as complete with optional error
func (p *HTTPPayload) Complete(err error) {
	select {
	case p.done <- err:
	default:
	}
}
