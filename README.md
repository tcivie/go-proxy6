# IPv6 Proxy

A high-performance HTTP/HTTPS proxy that routes traffic through random IPv6 addresses from your subnet.

## Performance

```
High-Performance Pipeline Architecture:
├── HTTP/HTTPS Support: Full proxy for both protocols
├── Dynamic IPv6: Random addresses from your subnet  
├── Worker Scaling: Auto-scales with CPU cores (default: 2x)
├── Memory Usage: ~100-500MB RAM
└── Throughput: 10,000-50,000 req/s (hardware dependent)
```

### Pipeline Architecture

The proxy uses a high-performance pipeline with dynamic worker pools:

```
HTTP/HTTPS Request → IPv6Generator → TargetProcessor → ProxyProcessor → Response
                     (Random IPv6)   (Host Resolution) (HTTP/HTTPS Exec)
```

**Stage Breakdown:**
- **IPv6Generator**: Generates random IPv6 addresses from your subnet
- **TargetProcessor**: Resolves target host and determines HTTP/HTTPS protocol  
- **ProxyProcessor**: Executes HTTP requests or HTTPS CONNECT tunneling

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
- Docker and docker-compose installed
- Root privileges (automatically handled by Docker)

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
```

The proxy generates a new random IPv6 address from your subnet for each request, providing excellent IP rotation.