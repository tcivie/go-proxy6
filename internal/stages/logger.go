package stages

import (
	"go-proxy6/internal/pipeline"
	"log"
	"log/slog"
)

// LoggerStage logs requests for debugging
type LoggerStage struct{}

func NewLogger() *LoggerStage {
	return &LoggerStage{}
}

func (s *LoggerStage) Name() string {
	return "Logger"
}

func (s *LoggerStage) Process(req *pipeline.Request) error {
	slog.Debug(req.ID,
		slog.String("method", req.Data.HTTPReq.Method),
		slog.String("host", req.Data.HTTPReq.Host),
		slog.String("path", req.Data.HTTPReq.URL.Path),
		slog.String("target", req.Data.Target),
		slog.String("bind", req.Data.BindAddr.IP.String()),
		slog.String("client", req.Data.HTTPReq.RemoteAddr),
		slog.String("user-agent", req.Data.HTTPReq.UserAgent()),
		slog.String("referer", req.Data.HTTPReq.Referer()),
		slog.String("x-forwarded-for", req.Data.HTTPReq.Header.Get("X-Forwarded-For")))
	httpReq := req.Data.HTTPReq
	log.Printf("[%s] %s %s %s", req.ID, httpReq.Method, httpReq.Host, httpReq.URL.Path)
	return nil
}
