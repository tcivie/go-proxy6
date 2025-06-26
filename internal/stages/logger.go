package stages

import (
	"go-proxy6/internal/pipeline"
	"log"
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
	httpReq := req.Data.HTTPReq
	log.Printf("[%s] %s %s %s", req.ID, httpReq.Method, httpReq.Host, httpReq.URL.Path)
	return nil
}
