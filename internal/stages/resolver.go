package stages

import (
	"fmt"
	"go-proxy6/internal/pipeline"
	"log"
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
