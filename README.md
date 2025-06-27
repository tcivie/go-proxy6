# IPv6 Proxy

A high-performance HTTP/HTTPS proxy that routes traffic through random IPv6 addresses from your subnet.

## Performance

Our optimized single-worker pipeline delivers exceptional performance:

```
Benchmark Results (Apple M2):
├── Throughput: 253,614 requests/second
├── Latency: 3.94μs per request
├── Memory: 1,705 B/op, 45 allocs/op
└── Goroutines: Only 3 total
```

### Pipeline Architecture

The proxy uses a high-performance pipeline with the following stages:

```
HTTP Request → IPv6Generator → TargetResolver → RequestExecutor → Response
                (511ns)         (459ns)          (2,973ns)
```

**Stage Breakdown:**
- **IPv6Generator** (13.0%): Generates random IPv6 addresses from your subnet with validation
- **TargetResolver** (11.6%): Resolves target host and validates request
- **RequestExecutor** (75.4%): Handles the actual HTTP proxy request with IPv6 binding

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
# Run all benchmarks
go test -bench=. ./internal/proxy/

# Run detailed benchmark with custom metrics
go test -bench=BenchmarkProxyPipelineDetailed ./internal/proxy/

# Run individual stage benchmarks
go test -bench=BenchmarkPipelineStages ./internal/proxy/
```

The benchmarks use mocked network calls to measure pure pipeline performance without network I/O variability.
