package stages

import (
	"fmt"
	"go-proxy6/internal/ipv6"
	"go-proxy6/internal/pipeline"
	"log"
)

// IPv6GeneratorStage generates random IPv6 addresses
type IPv6GeneratorStage struct {
	generator *ipv6.Generator
}

func NewIPv6Generator(gen *ipv6.Generator) *IPv6GeneratorStage {
	return &IPv6GeneratorStage{generator: gen}
}

func (s *IPv6GeneratorStage) Name() string {
	return "IPv6Generator"
}

func (s *IPv6GeneratorStage) Process(req *pipeline.Request) error {
	addr, err := s.generator.RandomAddr()
	if err != nil {
		return fmt.Errorf("IPv6 generation failed: %v", err)
	}

	req.Data.BindAddr = addr
	log.Printf("[%s] Generated IPv6: %s", req.ID, addr.IP)
	return nil
}
