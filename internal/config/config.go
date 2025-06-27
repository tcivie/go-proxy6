package config

import (
	"fmt"
	"net"
)

type Config struct {
	BindAddr   string
	IPv6Subnet string
	IPv6Net    *net.IPNet
	IPv6Base   net.IP
}

func New(bindAddr, ipv6Subnet string) (*Config, error) {
	ip, ipnet, err := net.ParseCIDR(ipv6Subnet)
	if err != nil {
		return nil, fmt.Errorf("invalid IPv6 subnet: %v", err)
	}
	if ip.To4() != nil {
		return nil, fmt.Errorf("must be IPv6 subnet")
	}

	return &Config{
		BindAddr:   bindAddr,
		IPv6Subnet: ipv6Subnet,
		IPv6Net:    ipnet,
		IPv6Base:   ip,
	}, nil
}
