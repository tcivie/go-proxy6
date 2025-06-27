# IPv6 Proxy

A high-performance HTTP (HTTPS Not supported yet) proxy that routes traffic through random IPv6 addresses from your subnet.

## Performance

```
Benchmark Results (Apple M2, Go 1.24):
├── Sequential: 180,051 ± 657 req/sec (5.56 ± 0.02μs latency)
├── Parallel: 264,705 ± 519 req/sec (3.78 ± 0.01μs latency)  
├── Memory: 1,953 ± 0.5 B/op, 48 allocs/op
└── Goroutines: Only 3 total
```

### Pipeline Architecture

The proxy uses a high-performance pipeline with the following stages:

```
HTTP Request → IPv6Generator → TargetResolver → RequestExecutor → Response
               (479 ± 2ns)    (425 ± 3ns)     (~4.65μs)
```

**Stage Breakdown (with 95% confidence intervals):**
- **IPv6Generator** (8.6%): 478.9 ± 1.9ns - Generates random IPv6 addresses from your subnet
- **TargetResolver** (7.6%): 424.7 ± 3.0ns - Resolves target host and validates request  
- **Pipeline Overhead** (83.8%): ~4.65μs - Request routing, channel communication, and mocking

## Quick Start

### Option 1: Docker Compose (Recommended)

1. **Configure your subnet** in `docker-compose.yml`:
   ```yaml
   environment:
     - SUBNET=YOUR_IPV6_SUBNET/64  # Replace with your actual subnet
   ```

2. **Deploy**:
   ```bash
   docker-compose up -d
   ```

3. **Scale up** (run multiple instances):
   ```bash
   # Run 3 proxy instances on different ports
   docker-compose up -d --scale ipv6-proxy=3
   ```

### Option 2: Docker (Manual)

```bash
# Build and run
docker build -t ipv6-proxy .
docker run --privileged --network host \
  -e SUBNET="YOUR_IPV6_SUBNET/64" \
  ipv6-proxy
```

### Option 3: Direct Run

```bash
# Build
go build -o ipv6-proxy

# Run
sudo ./ipv6-proxy -subnet="YOUR_IPV6_SUBNET/64"
```

## Usage

Once running, use as HTTP proxy on port 8080:

```bash
# Test with curl
curl -x http://127.0.0.1:8080 http://ipv6.icanhazip.com

# Use with applications
export http_proxy=http://127.0.0.1:8080
export https_proxy=http://127.0.0.1:8080
```

## Configuration

### Environment Variables (Docker)
- `SUBNET`: Your IPv6 subnet (required) - e.g., `2a01:4f9:c012:83eb::/64`
- `BIND`: Proxy listen address (default: `0.0.0.0:8080`)

### Command Line Flags (Direct run)
- `-bind`: Proxy listen address (default: `0.0.0.0:8080`)
- `-subnet`: Your IPv6 subnet (required)

## Requirements

- Linux server with IPv6 subnet (e.g., from Vultr, Hetzner, DigitalOcean)
- Docker and docker-compose installed
- Root privileges (automatically handled by Docker)

**How to get IPv6 subnet:**
1. Order VPS with IPv6 support from providers like Vultr, Hetzner
2. Your subnet will be something like `2a01:4f9:c012:83eb::/64`
3. Replace the `SUBNET` value in docker-compose.yml with your actual subnet

The proxy automatically configures the necessary IPv6 routing rules on startup.

## Benchmarking

To run performance benchmarks yourself:

```bash
# Run all benchmarks with statistical analysis
go test -bench=. -count=10 ./internal/proxy/

# Run detailed benchmark with custom metrics  
go test -bench=BenchmarkProxyPipelineDetailed -count=10 ./internal/proxy/

# Run individual stage benchmarks
go test -bench=BenchmarkPipelineStages -count=10 ./internal/proxy/

# For statistical comparison (install: go install golang.org/x/perf/cmd/benchstat@latest)
go test -bench=. -count=10 ./internal/proxy/ > results.txt
benchstat results.txt
```

The benchmarks use mocked network calls to measure pure pipeline performance without network I/O variability. All results include 95% confidence intervals from 10+ benchmark runs using Go 1.24's improved `testing.B.Loop` methodology.
