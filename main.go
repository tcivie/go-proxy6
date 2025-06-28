package main

import (
	"flag"
	"fmt"
	"go-proxy6/internal/config"
	"go-proxy6/internal/proxy"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
)

func main() {
	// Configure tint for beautiful colored logs
	logger := slog.New(
		tint.NewHandler(os.Stderr, &tint.Options{
			Level:      slog.LevelInfo,
			TimeFormat: time.StampMilli,
			AddSource:  false,
		}),
	)
	slog.SetDefault(logger)

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
		slog.Error("Configuration failed",
			"error", err,
			"bind_addr", *bindAddr,
			"ipv6_subnet", *ipv6Subnet,
		)
		os.Exit(1)
	}

	// Create and configure server
	server := proxy.NewServer(cfg)
	server.SetMaxWorkers(*workers)

	// Log startup configuration
	slog.Info("Starting IPv6 proxy server",
		"bind_addr", *bindAddr,
		"ipv6_subnet", *ipv6Subnet,
		"max_workers", *workers,
		"cpu_cores", runtime.NumCPU(),
	)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Info("Shutdown initiated",
			"signal", sig.String(),
		)
		server.Stop()
		slog.Info("Server stopped gracefully")
		os.Exit(0)
	}()

	// Start server
	slog.Info("Server starting...")
	if err := server.Start(); err != nil {
		slog.Error("Server failed to start",
			"error", err,
		)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("High-Performance IPv6 Proxy Server")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s [OPTIONS]\n", os.Args[0])
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -bind string")
	fmt.Println("        Proxy bind address (default \"localhost:8080\")")
	fmt.Println("  -subnet string")
	fmt.Println("        IPv6 subnet (default \"2001:19f0:6001:48e4::/64\")")
	fmt.Println("  -workers int")
	fmt.Printf("        Max number of workers (default %d)\n", runtime.NumCPU()*2)
	fmt.Println("  -help")
	fmt.Println("        Show this help")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s\n", os.Args[0])
	fmt.Printf("  %s -bind=:9090 -workers=16\n", os.Args[0])
	fmt.Printf("  %s -subnet=2001:db8::/64 -workers=8\n", os.Args[0])
	fmt.Println()
	fmt.Println("Features:")
	fmt.Println("  • HTTP and HTTPS proxy support")
	fmt.Println("  • Dynamic IPv6 address generation")
	fmt.Println("  • High-performance async processing")
	fmt.Println("  • Automatic worker scaling")
	fmt.Printf("  • Optimized for %d CPU cores\n", runtime.NumCPU())
}
