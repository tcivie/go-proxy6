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

// Enhanced benchmark with detailed metrics
func BenchmarkProxyPipelineDetailed(b *testing.B) {
	// Disable logging
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

	// Warm up
	for i := 0; i < 1000; i++ {
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

	// Reset counters after warmup
	atomic.StoreInt64(&processedCount, 0)

	b.ResetTimer()
	b.ReportAllocs()

	start := time.Now()
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

	elapsed := time.Since(start)
	processed := atomic.LoadInt64(&processedCount)

	// Custom metrics
	b.ReportMetric(float64(processed)/elapsed.Seconds(), "req/sec")
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(processed), "ns/req")
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
}

// Benchmark pipeline stages individually
func BenchmarkPipelineStages(b *testing.B) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(1000),
	})))

	cfg, _ := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	httpReq, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	b.Run("IPv6Generator", func(b *testing.B) {
		stage := stages.NewIPv6Generator(gen)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := &pipeline.Request{
				ID:   "test",
				Ctx:  context.Background(),
				Data: &pipeline.RequestData{HTTPReq: httpReq},
				Done: make(chan error, 1),
			}
			stage.Process(req)
		}
	})

	b.Run("TargetResolver", func(b *testing.B) {
		stage := stages.NewTargetResolver()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := &pipeline.Request{
				ID:  "test",
				Ctx: context.Background(),
				Data: &pipeline.RequestData{
					HTTPReq:  httpReq,
					BindAddr: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0},
				},
				Done: make(chan error, 1),
			}
			stage.Process(req)
		}
	})
}

// Benchmark memory usage and allocations
func BenchmarkProxyPipelineMemory(b *testing.B) {
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		pipelineReq := &pipeline.Request{
			ID:   "test-id",
			Ctx:  ctx,
			Done: make(chan error, 1),
			Data: &pipeline.RequestData{HTTPReq: httpReq},
		}

		pipe.Process(pipelineReq)
		<-pipelineReq.Done
	}
}
