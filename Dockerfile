# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ipv6-proxy main.go

# Runtime stage
FROM alpine:latest

# Install necessary tools
RUN apk --no-cache add iproute2 curl

WORKDIR /app
COPY --from=builder /app/ipv6-proxy .

# Default subnet (override with -e SUBNET=your_subnet)
ENV SUBNET="2a01:4f9:c012:83eb::/64"
ENV BIND="0.0.0.0:8080"
ENV WORKERS="50"

# Setup script that configures IPv6 and starts proxy
COPY <<EOF /app/start.sh
#!/bin/sh
set -e

echo "Configuring IPv6-ONLY proxy for subnet: \$SUBNET"

# Verify IPv6 is available
if ! ip -6 addr show | grep -q "inet6"; then
    echo "ERROR: IPv6 not available - aborting to prevent IP leakage"
    exit 1
fi

# Extract interface name (usually eth0 in container)
INTERFACE=\$(ip route | grep default | awk '{print \$5}' | head -n1)
echo "Using interface: \$INTERFACE"

# Enable non-local bind (required for IPv6 subnet binding)
sysctl net.ipv6.ip_nonlocal_bind=1

# Add local route for the IPv6 subnet
ip route add local \$SUBNET dev \$INTERFACE 2>/dev/null || echo "Route already exists"

# Verify the subnet route exists
if ! ip route show | grep -q "\$SUBNET"; then
    echo "ERROR: IPv6 subnet route not configured - aborting"
    exit 1
fi

echo "IPv6-only proxy starting - no IPv4 fallback allowed"
exec ./ipv6-proxy -bind="\$BIND" -subnet="\$SUBNET" -workers=\$WORKERS
EOF

RUN chmod +x /app/start.sh

EXPOSE 8080

CMD ["/app/start.sh"]