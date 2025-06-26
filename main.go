package main

import (
	"flag"
	"go-proxy6/internal/config"
	"go-proxy6/internal/proxy"
	"log"
)

func main() {
	var bindAddr = flag.String("bind", "0.0.0.0:8080", "Proxy bind address")
	var ipv6Subnet = flag.String("subnet", "2001:19f0:6001:48e4::/64", "IPv6 subnet")
	var workers = flag.Int("workers", 50, "Number of workers")
	flag.Parse()

	cfg, err := config.New(*bindAddr, *ipv6Subnet, *workers)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	server := proxy.NewServer(cfg)
	log.Printf("Starting proxy on %s with %d workers for this subnet: %s", cfg.BindAddr, cfg.Workers, cfg.IPv6Subnet)

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
