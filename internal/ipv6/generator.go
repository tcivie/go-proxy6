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
	return &Generator{network: network, base: base}
}

func (g *Generator) RandomAddr() (*net.TCPAddr, error) {
	ip, err := g.randomIP()
	if err != nil {
		return nil, err
	}
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
