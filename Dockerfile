# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o ipv6-proxy main.go

# Runtime stage
FROM alpine:latest

# Install necessary tools
RUN apk --no-cache add iproute2 curl

WORKDIR /app
COPY --from=builder /app/ipv6-proxy .

# Default configuration (override with environment variables)
ENV SUBNET="2a01:4f9:c012:83eb::/64"
ENV BIND="0.0.0.0:8080"
ENV WORKERS=""

# Setup script that configures IPv6 and starts proxy
RUN <<'EOT' cat > /app/start.sh
#!/bin/sh
set -e

echo "Configuring IPv6 for subnet: $SUBNET"

# Get the main interface (simpler approach)
INTERFACE=$(ip route | grep default | awk '{print $5}')

echo "Using interface: $INTERFACE"

# Enable IPv6
sysctl -w net.ipv6.conf.all.forwarding=1
sysctl -w net.ipv6.ip_nonlocal_bind=1

# Add route for the subnet
ip -6 route add local $SUBNET dev $INTERFACE 2>/dev/null || true

# Start the proxy
exec ./ipv6-proxy -bind=$BIND -subnet=$SUBNET
EOT

RUN chmod +x /app/start.sh

EXPOSE 8080

CMD ["/app/start.sh"]