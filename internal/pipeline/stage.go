package pipeline

import (
	"context"
	"fmt"
	"log/slog"
)

type dynamicWorkerPool struct {
	proc      Processor
	tokenPool chan struct{}
}

// DynamicWorkerPool returns a StageRunner that maintains a dynamic worker pool
// that can scale up to maxWorkers for processing incoming inputs in parallel
// and emitting their outputs to the next stage.
func DynamicWorkerPool(proc Processor, maxWorkers int) StageRunner {
	if maxWorkers <= 0 {
		panic("DynamicWorkerPool: maxWorkers must be > 0")
	}

	tokenPool := make(chan struct{}, maxWorkers)
	for i := 0; i < maxWorkers; i++ {
		tokenPool <- struct{}{}
	}

	return &dynamicWorkerPool{proc: proc, tokenPool: tokenPool}
}

// Run implements StageRunner.
func (p *dynamicWorkerPool) Run(ctx context.Context, params StageParams) {
stop:
	for {
		select {
		case <-ctx.Done():
			// Asked to cleanly shut down
			break stop
		case payloadIn, ok := <-params.Input():
			if !ok {
				break stop
			}

			var token struct{}
			select {
			case token = <-p.tokenPool:
			case <-ctx.Done():
				break stop
			}

			go func(payloadIn Payload, token struct{}) {
				defer func() {
					p.tokenPool <- token

					// Recover from any panics in processors to prevent crashing
					if r := recover(); r != nil {
						slog.Error("Pipeline stage panic recovered",
							"stage_index", params.StageIndex(),
							"panic", r)

						// Try to handle the payload if it's an HTTP payload
						if httpPayload, ok := payloadIn.(*HTTPPayload); ok {
							if !httpPayload.ResponseSent() {
								httpPayload.Response.WriteHeader(500)
								httpPayload.Response.Write([]byte("Internal server error"))
								httpPayload.MarkResponseSent()
							}
							httpPayload.Complete(fmt.Errorf("processor panic: %v", r))
						}
						payloadIn.MarkAsProcessed()
					}
				}()

				payloadOut, err := p.proc.Process(ctx, payloadIn)
				if err != nil {
					// Log the error but don't send it to error channel unless it's critical
					slog.Warn("Pipeline stage error (handled)",
						"stage_index", params.StageIndex(),
						"error", err)

					// For HTTP payloads, ensure proper completion
					if httpPayload, ok := payloadIn.(*HTTPPayload); ok {
						if !httpPayload.IsComplete() {
							httpPayload.Complete(err)
						}
					}

					payloadIn.MarkAsProcessed()
					return
				}

				// If the processor did not output a payload for the
				// next stage there is nothing we need to do.
				if payloadOut == nil {
					payloadIn.MarkAsProcessed()
					return
				}

				// Output processed data
				select {
				case params.Output() <- payloadOut:
				case <-ctx.Done():
					// Context cancelled, mark as processed
					payloadIn.MarkAsProcessed()
				}
			}(payloadIn, token)
		}
	}

	// Wait for all workers to exit by trying to empty the token pool
	for i := 0; i < cap(p.tokenPool); i++ {
		<-p.tokenPool
	}
}
