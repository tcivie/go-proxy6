package pipeline

import (
	"context"
	"fmt"
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
				defer func() { p.tokenPool <- token }()
				payloadOut, err := p.proc.Process(ctx, payloadIn)
				if err != nil {
					wrappedErr := fmt.Errorf("pipeline stage %d: %w", params.StageIndex(), err)
					maybeEmitError(wrappedErr, params.Error())
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
				}
			}(payloadIn, token)
		}
	}

	// Wait for all workers to exit by trying to empty the token pool
	for i := 0; i < cap(p.tokenPool); i++ {
		<-p.tokenPool
	}
}
