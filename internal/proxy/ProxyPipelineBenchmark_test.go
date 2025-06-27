package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"go-proxy6/internal/config"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"go-proxy6/internal/pipeline/mocks"
	"go-proxy6/internal/stages"

	"go.uber.org/mock/gomock"
)

// Simple source for benchmarking
type benchmarkSource struct {
	payloads []*pipeline.HTTPPayload
	index    int
}

func newBenchmarkSource(count int) *benchmarkSource {
	payloads := make([]*pipeline.HTTPPayload, count)
	for i := 0; i < count; i++ {
		req := httptest.NewRequest("GET", "http://example.com", nil)
		resp := httptest.NewRecorder()
		payloads[i] = pipeline.NewHTTPPayload(fmt.Sprintf("bench-%d", i), context.Background(), req, resp)
	}
	return &benchmarkSource{payloads: payloads}
}

func (s *benchmarkSource) Next(ctx context.Context) bool {
	if s.index >= len(s.payloads) {
		return false
	}
	s.index++
	return true
}

func (s *benchmarkSource) Payload() pipeline.Payload {
	if s.index <= 0 || s.index > len(s.payloads) {
		return nil
	}
	return s.payloads[s.index-1]
}

func (s *benchmarkSource) Error() error {
	return nil
}

// Simple sink for benchmarking
type benchmarkSink struct{}

func (s *benchmarkSink) Consume(ctx context.Context, payload pipeline.Payload) error {
	return nil
}

// Main pipeline benchmark
func BenchmarkProxyPipeline(b *testing.B) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	cfg, err := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	if err != nil {
		b.Fatalf("Failed to create config: %v", err)
	}

	var processedCount int64
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)

	// Simple mock processor
	mockProcessor := mocks.NewMockProcessor(ctrl)
	mockProcessor.EXPECT().Process(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, payload pipeline.Payload) (pipeline.Payload, error) {
			atomic.AddInt64(&processedCount, 1)
			if httpPayload, ok := payload.(*pipeline.HTTPPayload); ok {
				httpPayload.Complete(nil)
			}
			return nil, nil
		},
	).AnyTimes()

	pipe := pipeline.New(
		pipeline.DynamicWorkerPool(stages.NewIPv6Processor(gen), 4),
		pipeline.DynamicWorkerPool(stages.NewTargetProcessor(), 4),
		pipeline.DynamicWorkerPool(mockProcessor, 2),
	)

	source := newBenchmarkSource(b.N)
	sink := &benchmarkSink{}

	b.ReportAllocs()
	b.ResetTimer()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = pipe.Process(ctx, source, sink)
	if err != nil {
		b.Fatalf("Pipeline failed: %v", err)
	}

	// Report metrics
	processed := atomic.LoadInt64(&processedCount)
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
	if processed > 0 {
		b.ReportMetric(float64(processed), "processed")
	}
}

// Individual stage benchmarks
func BenchmarkIPv6Generator(b *testing.B) {
	cfg, _ := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	processor := stages.NewIPv6Processor(gen)

	req := httptest.NewRequest("GET", "http://example.com", nil)
	resp := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		payload := pipeline.NewHTTPPayload("test", context.Background(), req, resp)
		result, err := processor.Process(context.Background(), payload)
		if err != nil {
			b.Fatal(err)
		}
		if result.(*pipeline.HTTPPayload).BindAddr == nil {
			b.Fatal("BindAddr not set")
		}
	}
}

func BenchmarkTargetProcessor(b *testing.B) {
	processor := stages.NewTargetProcessor()
	req := httptest.NewRequest("GET", "http://example.com", nil)
	resp := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		payload := pipeline.NewHTTPPayload("test", context.Background(), req, resp)
		payload.BindAddr = &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}

		result, err := processor.Process(context.Background(), payload)
		if err != nil {
			b.Fatal(err)
		}
		if result.(*pipeline.HTTPPayload).Target == "" {
			b.Fatal("Target not set")
		}
	}
}

// Memory benchmark
func BenchmarkMemoryUsage(b *testing.B) {
	cfg, _ := config.New("127.0.0.1:8080", "2001:19f0:6001:48e4::/64")
	gen := ipv6.NewGenerator(cfg.IPv6Net, cfg.IPv6Base)
	ipv6Proc := stages.NewIPv6Processor(gen)
	targetProc := stages.NewTargetProcessor()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "http://example.com", nil)
		resp := httptest.NewRecorder()
		payload := pipeline.NewHTTPPayload("test", context.Background(), req, resp)

		// Process through stages
		result1, _ := ipv6Proc.Process(context.Background(), payload)
		_, _ = targetProc.Process(context.Background(), result1)
	}
}

// Basic functionality test
func TestBasicFunctionality(t *testing.T) {
	cfg, err := config.New("127.0.0.1:0", "2001:db8::/64")
	if err != nil {
		t.Fatalf("Config creation failed: %v", err)
	}

	server := NewServer(cfg)
	server.SetMaxWorkers(2)

	if server.maxWorkers != 2 {
		t.Errorf("Expected 2 workers, got %d", server.maxWorkers)
	}

	// Test payload lifecycle
	req := httptest.NewRequest("GET", "http://example.com", nil)
	resp := httptest.NewRecorder()
	payload := pipeline.NewHTTPPayload("test", context.Background(), req, resp)

	payload.Complete(nil)
	select {
	case err := <-payload.Done():
		if err != nil {
			t.Errorf("Expected nil error, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Payload completion timed out")
	}
}
