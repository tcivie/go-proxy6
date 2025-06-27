package main

import (
	"flag"
	"go-proxy6/internal/config"
	"go-proxy6/internal/proxy"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	var (
		bindAddr   = flag.String("bind", "localhost:8080", "Proxy bind address")
		ipv6Subnet = flag.String("subnet", "2001:19f0:6001:48e4::/64", "IPv6 subnet")
		workers    = flag.Int("workers", runtime.NumCPU()*2, "Max number of workers")
		showHelp   = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	// Validate workers
	if *workers <= 0 {
		*workers = runtime.NumCPU() * 2
	}

	// Create configuration
	cfg, err := config.New(*bindAddr, *ipv6Subnet)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Create and configure server
	server := proxy.NewServer(cfg)
	server.SetMaxWorkers(*workers)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		server.Stop()
		os.Exit(0)
	}()

	// Start server
	log.Fatal(server.Start())
}

func printHelp() {
	log.Println("High-Performance IPv6 Proxy Server")
	log.Println()
	log.Println("Usage:")
	log.Printf("  %s [OPTIONS]\n", os.Args[0])
	log.Println()
	log.Println("Options:")
	log.Println("  -bind string")
	log.Println("        Proxy bind address (default \"0.0.0.0:8080\")")
	log.Println("  -subnet string")
	log.Println("        IPv6 subnet (default \"2001:19f0:6001:48e4::/64\")")
	log.Println("  -workers int")
	log.Printf("        Max number of workers (default %d)\n", runtime.NumCPU()*2)
	log.Println("  -help")
	log.Println("        Show this help")
	log.Println()
	log.Println("Examples:")
	log.Printf("  %s\n", os.Args[0])
	log.Printf("  %s -bind=:9090 -workers=16\n", os.Args[0])
	log.Printf("  %s -subnet=2001:db8::/64 -workers=8\n", os.Args[0])
	log.Println()
	log.Println("Features:")
	log.Println("  • HTTP and HTTPS proxy support")
	log.Println("  • Dynamic IPv6 address generation")
	log.Println("  • High-performance async processing")
	log.Println("  • Automatic worker scaling")
	log.Printf("  • Optimized for %d CPU cores\n", runtime.NumCPU())
}
