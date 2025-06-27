package proxy

import (
	"context"
	"fmt"
	"go-proxy6/internal/config"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"go-proxy6/internal/pipeline/mocks"
	"go-proxy6/internal/stages"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

func BenchmarkProxyPipelineDetailed(b *testing.B) {
	// Setup - this will be automatically excluded from timing by B.Loop
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(1000),
	})))

	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	var processedCount int64
	mockExecutor := mocks.NewMockStage(ctrl)
	mockExecutor.EXPECT().Name().Return("MockRequestExecutor").AnyTimes()
	mockExecutor.EXPECT().Process(gomock.Any()).DoAndReturn(func(req *pipeline.Request) error {
		atomic.AddInt64(&processedCount, 1)
		return nil
	}).AnyTimes()

	cfg, err := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	if err != nil {
		b.Fatalf("Failed to create config: %v", err)
	}

	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	pipe := pipeline.New(
		stages.NewIPv6Generator(gen),
		stages.NewTargetResolver(),
		mockExecutor,
	)

	pipe.Start()
	defer pipe.Stop()

	httpReq, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		b.Fatalf("Failed to create HTTP request: %v", err)
	}

	// Warm up - excluded from timing by B.Loop
	for i := 0; i < 100; i++ {
		ctx := context.Background()
		pipelineReq := &pipeline.Request{
			ID:   fmt.Sprintf("warmup-%d", i),
			Ctx:  ctx,
			Done: make(chan error, 1),
			Data: &pipeline.RequestData{HTTPReq: httpReq},
		}
		pipe.Process(pipelineReq)
		<-pipelineReq.Done
	}

	atomic.StoreInt64(&processedCount, 0)
	b.ReportAllocs()

	var totalRequests int64
	startTime := time.Now()

	// B.Loop automatically handles timing and prevents dead code elimination
	for b.Loop() {
		ctx := context.Background()
		pipelineReq := &pipeline.Request{
			ID:   "bench-request",
			Ctx:  ctx,
			Done: make(chan error, 1),
			Data: &pipeline.RequestData{HTTPReq: httpReq},
		}

		pipe.Process(pipelineReq)
		select {
		case err := <-pipelineReq.Done:
			if err != nil {
				b.Fatalf("Unexpected pipeline error: %v", err)
			}
			totalRequests++
		case <-time.After(time.Second):
			b.Fatalf("Pipeline processing timed out")
		}
	}

	// Report custom metrics - excluded from timing by B.Loop
	elapsed := time.Since(startTime)
	processed := atomic.LoadInt64(&processedCount)

	if elapsed > 0 && processed > 0 {
		reqPerSec := float64(processed) / elapsed.Seconds()
		nsPerReq := float64(elapsed.Nanoseconds()) / float64(processed)

		b.ReportMetric(reqPerSec, "req/sec")
		b.ReportMetric(nsPerReq, "ns/req")
	}
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
}

// Parallel benchmark using B.Loop
func BenchmarkProxyPipelineParallel(b *testing.B) {
	// Setup
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(1000),
	})))

	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	mockExecutor := mocks.NewMockStage(ctrl)
	mockExecutor.EXPECT().Name().Return("MockRequestExecutor").AnyTimes()
	mockExecutor.EXPECT().Process(gomock.Any()).Return(nil).AnyTimes()

	cfg, err := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	if err != nil {
		b.Fatalf("Failed to create config: %v", err)
	}

	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	pipe := pipeline.New(
		stages.NewIPv6Generator(gen),
		stages.NewTargetResolver(),
		mockExecutor,
	)

	pipe.Start()
	defer pipe.Stop()

	httpReq, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		b.Fatalf("Failed to create HTTP request: %v", err)
	}

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := context.Background()
			pipelineReq := &pipeline.Request{
				ID:   "test-id",
				Ctx:  ctx,
				Done: make(chan error, 1),
				Data: &pipeline.RequestData{HTTPReq: httpReq},
			}

			pipe.Process(pipelineReq)
			select {
			case err := <-pipelineReq.Done:
				if err != nil {
					b.Fatalf("Unexpected pipeline error: %v", err)
				}
			case <-ctx.Done():
				b.Fatalf("Pipeline processing timed out")
			}
		}
	})
}

// Benchmark pipeline stages individually using B.Loop
func BenchmarkPipelineStages(b *testing.B) {
	// Setup
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(1000),
	})))

	cfg, _ := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	httpReq, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	b.Run("IPv6Generator", func(b *testing.B) {
		stage := stages.NewIPv6Generator(gen)

		for b.Loop() {
			req := &pipeline.Request{
				ID:   "test",
				Ctx:  context.Background(),
				Data: &pipeline.RequestData{HTTPReq: httpReq},
				Done: make(chan error, 1),
			}
			err := stage.Process(req)
			// Ensure the result is used to prevent dead code elimination
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("TargetResolver", func(b *testing.B) {
		stage := stages.NewTargetResolver()

		for b.Loop() {
			req := &pipeline.Request{
				ID:  "test",
				Ctx: context.Background(),
				Data: &pipeline.RequestData{
					HTTPReq:  httpReq,
					BindAddr: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0},
				},
				Done: make(chan error, 1),
			}
			err := stage.Process(req)
			// Ensure the result is used to prevent dead code elimination
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark memory usage and allocations using B.Loop
func BenchmarkProxyPipelineMemory(b *testing.B) {
	// Setup
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(1000),
	})))

	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	mockExecutor := mocks.NewMockStage(ctrl)
	mockExecutor.EXPECT().Name().Return("MockRequestExecutor").AnyTimes()
	mockExecutor.EXPECT().Process(gomock.Any()).Return(nil).AnyTimes()

	cfg, _ := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	pipe := pipeline.New(stages.NewIPv6Generator(gen), stages.NewTargetResolver(), mockExecutor)
	pipe.Start()
	defer pipe.Stop()

	httpReq, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	b.ReportAllocs()

	for b.Loop() {
		ctx := context.Background()
		pipelineReq := &pipeline.Request{
			ID:   "test-id",
			Ctx:  ctx,
			Done: make(chan error, 1),
			Data: &pipeline.RequestData{HTTPReq: httpReq},
		}

		pipe.Process(pipelineReq)
		err := <-pipelineReq.Done
		// Ensure the result is used to prevent dead code elimination
		if err != nil {
			b.Fatal(err)
		}
	}
}
