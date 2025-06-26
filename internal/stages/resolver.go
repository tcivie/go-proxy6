package stages

import (
	"fmt"
	"go-proxy6/internal/pipeline"
	"log"
	"log/slog"
	"net/http"
	"strings"
)

// TargetResolverStage resolves target addresses from HTTP requests
type TargetResolverStage struct{}

func NewTargetResolver() *TargetResolverStage {
	return &TargetResolverStage{}
}

func (s *TargetResolverStage) Name() string {
	return "TargetResolver"
}

func (s *TargetResolverStage) Process(req *pipeline.Request) error {
	httpReq := req.Data.HTTPReq
	slog.Debug(req.ID,
		slog.String("method", httpReq.Method),
		slog.String("host", httpReq.Host),
		slog.String("path", httpReq.URL.Path),
		slog.String("target", req.Data.Target),
		slog.String("bind", req.Data.BindAddr.IP.String()),
		slog.String("client", httpReq.RemoteAddr),
		slog.String("user-agent", httpReq.UserAgent()),
		slog.String("referer", httpReq.Referer()),
		slog.String("x-forwarded-for", httpReq.Header.Get("X-Forwarded-For")))

	var target string
	if httpReq.Method == http.MethodConnect {
		target = httpReq.Host
		if !strings.Contains(target, ":") {
			target += ":443"
		}
	} else {
		if httpReq.URL.Host == "" {
			return fmt.Errorf("missing host header")
		}
		target = httpReq.URL.Host
		if !strings.Contains(target, ":") {
			target += ":80"
		}
	}

	req.Data.Target = target
	bindAddr := req.Data.BindAddr
	log.Printf("[%s] Target: %s via %s", req.ID, target, bindAddr.IP)
	return nil
}
