# IPv6 Proxy

A high-performance **IPv6-only** HTTP/HTTPS proxy that routes traffic through random IPv6 addresses from your subnet.

## Security Matrix

| Protocol | Target | Action |
|----------|--------|--------|
| HTTP | IPv4 | ❌ **BLOCKED** |
| HTTPS | IPv4 | ❌ **BLOCKED** |
| HTTP | IPv6 | ✅ **PROXIED** |
| HTTPS | IPv6 | ✅ **PROXIED** |

**By Design**: IPv4 requests are blocked to prevent IP leakage. Only IPv6 traffic is proxied.

## Performance

```
Benchmark Results (Apple M2, Go 1.21):
├── Pipeline Throughput: 199,588 req/sec (5.9μs latency)
├── IPv6 Generation: 4,161,603 ops/sec (282ns per address)
├── Target Resolution: 7,660,347 ops/sec (155ns per resolve)
├── Memory Efficiency: 792 B/op, 25 allocs/op
└── Resource Usage: 2 goroutines per pipeline
```

## Quick Start

### Docker Compose (Recommended)

1. **Configure** `docker-compose.yml`:
   ```yaml
   environment:
     - SUBNET=YOUR_IPV6_SUBNET/64  # Replace with your subnet
     - BIND=0.0.0.0:8080          # Optional: change port
   ```

2. **Deploy**:
   ```bash
   docker-compose up -d
   ```

### Direct Run

```bash
go build -o ipv6-proxy main.go
sudo ./ipv6-proxy -subnet="YOUR_IPV6_SUBNET/64" -bind="0.0.0.0:8080"
```

## Usage

```bash
# Test proxy
curl -x http://127.0.0.1:8080 http://ipv6.icanhazip.com

# Configure applications
export http_proxy=http://127.0.0.1:8080
export https_proxy=http://127.0.0.1:8080
```

## Configuration

### Environment Variables (Docker)
- `SUBNET`: Your IPv6 subnet (required) - e.g., `2a01:4f9:c012:83eb::/64`
- `BIND`: Listen address (default: `0.0.0.0:8080`)
- `WORKERS`: Worker count (default: auto)

### Command Line Flags
- `-subnet`: IPv6 subnet (required)
- `-bind`: Listen address (default: `localhost:8080`)
- `-workers`: Worker count (default: 2x CPU cores)

## Requirements

- Linux VPS with IPv6 subnet (Vultr, Hetzner, DigitalOcean)
- Docker + docker-compose
- Root privileges (for IPv6 routing)

**Getting IPv6 Subnet:**
1. Order VPS with IPv6 from cloud provider
2. Find your subnet (format: `2a01:4f9:c012:83eb::/64`)
3. Update `SUBNET` in docker-compose.yml

## Testing

```bash
# Performance test
go test -bench=. ./internal/proxy/

# IP rotation test
for i in {1..5}; do curl -x http://127.0.0.1:8080 http://ipv6.icanhazip.com; done
```