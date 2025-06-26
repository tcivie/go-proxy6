package ipv6

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"time"
)

type Generator struct {
	network    *net.IPNet
	base       net.IP
	maxRetries int
}

func NewGenerator(network *net.IPNet, base net.IP) *Generator {
	return &Generator{
		network:    network,
		base:       base,
		maxRetries: 10,
	}
}

func (g *Generator) RandomAddr() (*net.TCPAddr, error) {
	ip, err := g.randomIP()
	if err != nil {
		slog.Error("Failed to generate random IP", "error", err.Error())
		return nil, err
	}

	slog.Info("Generated IPv6 address", "ip", ip.String())
	return &net.TCPAddr{IP: ip, Port: 0}, nil
}

func (g *Generator) randomIP() (net.IP, error) {
	ones, bits := g.network.Mask.Size()
	if bits != 128 {
		return nil, fmt.Errorf("invalid IPv6 mask")
	}

	hostBits := bits - ones
	hostBytes := (hostBits + 7) / 8

	randomBytes := make([]byte, hostBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}

	ip := make(net.IP, len(g.base))
	copy(ip, g.base)

	byteOffset := ones / 8
	bitOffset := ones % 8

	for i, b := range randomBytes {
		if byteOffset+i >= len(ip) {
			break
		}
		if bitOffset == 0 {
			ip[byteOffset+i] = b
		} else {
			mask := byte(0xFF >> bitOffset)
			ip[byteOffset+i] = (ip[byteOffset+i] & ^mask) | (b & mask)
		}
	}

	return ip, nil
}

func (g *Generator) isValidIPv6(ip net.IP) bool {
	// Try to bind to the address to check if it's actually usable
	addr := &net.TCPAddr{IP: ip, Port: 0}
	listener, err := net.ListenTCP("tcp6", addr)
	if err != nil {
		slog.Warn("TCP bind test failed",
			"ip", ip.String(),
			"error", err.Error(),
			"error_type", fmt.Sprintf("%T", err))
		return false
	}
	listener.Close()
	slog.Debug("TCP bind test succeeded", "ip", ip.String())
	return true
}
