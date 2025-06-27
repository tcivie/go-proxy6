package ipv6

import (
	"net"
	"testing"
)

func TestGenerator_RandomAddr(t *testing.T) {
	cases := []struct {
		name       string
		network    *net.IPNet
		base       net.IP
		shouldFail bool
	}{
		{
			name: "ValidRandomAddr",
			network: &net.IPNet{
				IP:   net.ParseIP("2001:db8::"),
				Mask: net.CIDRMask(64, 128),
			},
			base:       net.ParseIP("2001:db8::"),
			shouldFail: false,
		},
		{
			name: "InvalidMask",
			network: &net.IPNet{
				IP:   net.ParseIP("2001:db8::"),
				Mask: net.CIDRMask(63, 128),
			},
			base:       net.ParseIP("2001:db8::"),
			shouldFail: true,
		},
		{
			name: "ZeroHostBits",
			network: &net.IPNet{
				IP:   net.ParseIP("2001:db8::"),
				Mask: net.CIDRMask(128, 128),
			},
			base:       net.ParseIP("2001:db8::"),
			shouldFail: false,
		},
		{
			name: "FullRange",
			network: &net.IPNet{
				IP:   net.ParseIP("::"),
				Mask: net.CIDRMask(0, 128),
			},
			base:       net.ParseIP("::"),
			shouldFail: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(tc.network, tc.base)
			addr, err := g.RandomAddr()

			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected failure, got success. Addr: %+v", addr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !tc.network.Contains(addr.IP) {
					t.Errorf("generated IP %v not in range %v", addr.IP, tc.network)
				}
			}
		})
	}
}
