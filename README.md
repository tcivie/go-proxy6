# IPv6 Proxy

A high-performance **IPv6-only** HTTP/HTTPS proxy that routes traffic through random IPv6 addresses from your subnet.

## Security Features

🔒 **IPv6-Only Operation**: No IPv4 fallback to prevent IP leakage  
🔒 **Subnet Validation**: Only uses addresses from your specified IPv6 subnet  
🔒 **Fail-Safe Design**: Fails fast if IPv6 is not available  
🔒 **No DNS Leaks**: All connections forced through IPv6  

## Performance

```
Benchmark Results (Apple M2, Go 1.24):
├── Pipeline Throughput: 179,602 req/sec (5.9μs latency)
├── IPv6 Generation: 5,448,044 ops/sec (219ns per address)
├── Target Resolution: 9,412,366 ops/sec (163ns per resolve)
├── Memory Efficiency: 680 B/op, 20 allocs/op
└── Resource Usage: 2 goroutines, minimal overhead
```

### Pipeline Architecture

The proxy uses a high-performance pipeline with dynamic worker pools:

```
HTTP/HTTPS Request → IPv6Generator → TargetProcessor → ProxyProcessor → Response
                     (219ns)        (163ns)          (HTTP/HTTPS Exec)
```

**Stage Breakdown:**
- **IPv6Generator** (219ns): Generates random IPv6 addresses from your subnet
- **TargetProcessor** (163ns): Resolves target host and determines HTTP/HTTPS protocol  
- **ProxyProcessor**: Executes HTTP requests or HTTPS CONNECT tunneling

**Performance Characteristics:**
- **Latency**: 5.9μs per request through full pipeline
- **Memory**: 680 bytes per operation, 20 allocations
- **Scalability**: Auto-scales with CPU cores (default: 2x)
- **Efficiency**: Only 2 goroutines for pipeline coordination

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
go build -o ipv6-proxy main.go

# Run
sudo ./ipv6-proxy -subnet="YOUR_IPV6_SUBNET/64"
```

## Usage

Once running, use as HTTP/HTTPS proxy on port 8080:

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
- `WORKERS`: Number of workers (default: auto-detected)

### Command Line Flags (Direct run)
- `-bind`: Proxy listen address (default: `0.0.0.0:8080`)
- `-subnet`: Your IPv6 subnet (required)
- `-workers`: Number of workers (default: 2x CPU cores)

## Requirements

- Linux server with IPv6 subnet (e.g., from Vultr, Hetzner, DigitalOcean)
- **IPv6-only environment** - no IPv4 fallback for security
- Docker and docker-compose installed
- Root privileges (automatically handled by Docker)

**⚠️ Security Note:** This proxy operates in IPv6-only mode to prevent IP leakage. If IPv6 is not properly configured, the proxy will fail rather than fall back to IPv4.

**How to get IPv6 subnet:**
1. Order VPS with IPv6 support from providers like Vultr, Hetzner
2. Your subnet will be something like `2a01:4f9:c012:83eb::/64`
3. Replace the `SUBNET` value in docker-compose.yml with your actual subnet

The proxy automatically configures the necessary IPv6 routing rules on startup.

## Performance Testing

To test performance yourself:

```bash
# Basic performance test
curl -x http://127.0.0.1:8080 http://httpbin.org/ip

# Load testing with Apache Bench
ab -n 1000 -c 10 -X 127.0.0.1:8080 http://httpbin.org/ip

# Check different IPv6 addresses
for i in {1..5}; do
  curl -x http://127.0.0.1:8080 http://ipv6.icanhazip.com
done

# Run your own benchmarks
go test -bench=. ./internal/proxy/
```

The proxy generates a new random IPv6 address from your subnet for each request, providing excellent IP rotation with minimal performance impact.