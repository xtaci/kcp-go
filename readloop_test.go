package kcp

import (
	"net"
	"testing"
)

// netAddrTestAddr wraps an existing address with a custom net.Addr type.
type netAddrTestAddr struct {
	addr net.Addr
}

func (a netAddrTestAddr) Network() string {
	return a.addr.Network()
}

func (a netAddrTestAddr) String() string {
	return a.addr.String()
}

// netAddrTestStringAddr is a custom address backed by a string.
type netAddrTestStringAddr string

func (a netAddrTestStringAddr) Network() string {
	return "test"
}

func (a netAddrTestStringAddr) String() string {
	return string(a)
}

func TestIsValidAddr(t *testing.T) {
	var nilAddr net.Addr
	var nilUDPAddr *net.UDPAddr

	tests := []struct {
		name string
		addr net.Addr
		want bool
	}{
		{"nil interface", nilAddr, false},
		{"nil UDP address", nilUDPAddr, false},
		{"UDP address", &net.UDPAddr{Port: 12345}, true},
		{"custom UDP address", netAddrTestAddr{&net.UDPAddr{Port: 12345}}, true},
		{"custom string address", netAddrTestStringAddr("peer:12345"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidAddr(tt.addr); got != tt.want {
				t.Fatalf("isValidAddr(%v) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIsSameAddr(t *testing.T) {
	udpAddr := &net.UDPAddr{
		IP:   net.IPv4(192, 0, 2, 1),
		Port: 12345,
	}

	sameUDPAddr := &net.UDPAddr{
		IP:   net.IPv4(192, 0, 2, 1),
		Port: 12345,
	}

	differentIP := &net.UDPAddr{
		IP:   net.IPv4(192, 0, 2, 2),
		Port: 12345,
	}

	differentPort := &net.UDPAddr{
		IP:   net.IPv4(192, 0, 2, 1),
		Port: 12346,
	}

	zoneAddr := &net.UDPAddr{
		IP:   net.ParseIP("fe80::1"),
		Port: 12345,
		Zone: "en0",
	}

	differentZone := &net.UDPAddr{
		IP:   net.ParseIP("fe80::1"),
		Port: 12345,
		Zone: "en1",
	}

	customAddr := netAddrTestStringAddr(udpAddr.String())

	var nilAddr net.Addr
	var nilUDPAddr *net.UDPAddr

	tests := []struct {
		name  string
		addr1 net.Addr
		addr2 net.Addr
		want  bool
	}{
		{"same UDP address", udpAddr, sameUDPAddr, true},
		{"different IP", udpAddr, differentIP, false},
		{"different port", udpAddr, differentPort, false},
		{"different IPv6 zone", zoneAddr, differentZone, false},

		{
			"same custom address",
			netAddrTestStringAddr("peer:12345"),
			netAddrTestStringAddr("peer:12345"),
			true,
		},
		{
			"different custom address",
			netAddrTestStringAddr("peer:12345"),
			netAddrTestStringAddr("peer:12346"),
			false,
		},
		{
			"different custom types with same string",
			netAddrTestAddr{addr: udpAddr},
			netAddrTestStringAddr(udpAddr.String()),
			true,
		},

		// A UDP address and a custom address are never considered equal,
		// even when their String() values are identical.
		{
			"UDP and custom address with same string",
			udpAddr,
			customAddr,
			false,
		},
		{
			"custom and UDP address with same string",
			customAddr,
			udpAddr,
			false,
		},
		{
			"UDP and custom address with different string",
			udpAddr,
			netAddrTestStringAddr("peer:12345"),
			false,
		},

		{"nil interface", nilAddr, udpAddr, false},
		{"nil UDP address", nilUDPAddr, udpAddr, false},
		{"nil UDP address on second argument", udpAddr, nilUDPAddr, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSameAddr(tt.addr1, tt.addr2); got != tt.want {
				t.Fatalf("isSameAddr(%v, %v) = %v, want %v",
					tt.addr1, tt.addr2, got, tt.want)
			}
		})
	}
}
