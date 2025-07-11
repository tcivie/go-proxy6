package ipv6

import (
	"crypto/rand"
	"fmt"
	"net"
)

type Generator struct {
	network *net.IPNet
	base    net.IP
}

func NewGenerator(network *net.IPNet, base net.IP) *Generator {
	return &Generator{
		network: network,
		base:    base,
	}
}

func (g *Generator) RandomAddr() (*net.TCPAddr, error) {
	ip, err := g.randomIP()
	if err != nil {
		return nil, err
	}

	// SECURITY: Ensure we only return IPv6 addresses
	if ip.To4() != nil {
		return nil, fmt.Errorf("security error: generated IPv4 address instead of IPv6")
	}

	return &net.TCPAddr{IP: ip}, nil
}

// randomIP generates a randomized IPv6 address within the generator's configured subnet.
func (g *Generator) randomIP() (net.IP, error) {
	if err := g.validateMask(); err != nil {
		return nil, err
	}

	if err := g.validateBaseIP(); err != nil {
		return nil, err
	}

	randomBytes, err := generateRandomBytes(g.hostBytes())
	if err != nil {
		return nil, err
	}

	return applyRandomHost(g.base, g.network.Mask.Size, randomBytes), nil
}

// validateMask checks if the network mask is valid for IPv6.
func (g *Generator) validateMask() error {
	_, bits := g.network.Mask.Size()
	if bits != 128 {
		return fmt.Errorf("invalid IPv6 mask")
	}
	return nil
}

// validateBaseIP checks if the base IP belongs to the specified network.
func (g *Generator) validateBaseIP() error {
	if !g.network.Contains(g.base) {
		return fmt.Errorf("base IP %v does not belong to network %v", g.base, g.network)
	}
	return nil
}

// hostBytes calculates the number of bytes required for the host portion of the IP address.
func (g *Generator) hostBytes() int {
	ones, bits := g.network.Mask.Size()
	return (bits - ones + 7) / 8
}

// applyRandomHost creates a new IP by injecting random host bits into the base IP.
func applyRandomHost(base net.IP, maskSize func() (int, int), randomBytes []byte) net.IP {
	ones, _ := maskSize()
	ip := make(net.IP, len(base))
	copy(ip, base)

	byteOffset, bitOffset := ones/8, ones%8
	applyBytes(ip, randomBytes, byteOffset, bitOffset)

	return ip
}

// applyBytes modifies the base IP by applying random bytes at a specific offset.
func applyBytes(ip net.IP, randomBytes []byte, byteOffset, bitOffset int) {
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
}

// generateRandomBytes creates a slice of random bytes.
func generateRandomBytes(size int) ([]byte, error) {
	randomBytes := make([]byte, size)
	_, err := rand.Read(randomBytes)
	return randomBytes, err
}
