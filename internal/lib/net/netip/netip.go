// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package netip defines an IP address type that's a small value type.
// Building on that [Addr] type, the package also defines [AddrPort] (an
// IP address and a port) and [Prefix] (an IP address and a bit length
// prefix).
//
// Compared to the [net.IP] type, [Addr] type takes less memory, is immutable,
// and is comparable (supports == and being a map key).
package netip

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
)

type Addr struct {
	// addr is the hi and lo bits of an IPv6 address. IPv4 addresses use
	// their IPv4-mapped IPv6 representation.
	//
	// hi and lo are constructed by interpreting a 16-byte IPv6
	// address as a big-endian 128-bit number. The most significant
	// bits of that number go into hi, the rest into lo.
	//
	// For example, 0011:2233:4455:6677:8899:aabb:ccdd:eeff is stored as:
	//  addr.hi = 0x0011223344556677
	//  addr.lo = 0x8899aabbccddeeff
	//
	// We store IPs like this, rather than as [16]byte, because it
	// turns most operations on IPs into arithmetic and bit-twiddling
	// operations on 64-bit registers, which is much faster than
	// bytewise processing.
	addr uint128
}

// IPv6Unspecified returns the IPv6 unspecified address "::".
func IPv6Unspecified() Addr { return Addr{} }

// IPv4Unspecified returns the IPv4 unspecified address "0.0.0.0".
func IPv4Unspecified() Addr { return AddrFrom4([4]byte{}) }

// IPv4Broadcast returns the limited IPv4 broadcast address.
func IPv4Broadcast() Addr { return AddrFrom4([4]byte{255, 255, 255, 255}) }

// AddrFrom4 returns the address of the IPv4 address given by the bytes in addr.
func AddrFrom4(addr [4]byte) Addr {
	return Addr{
		addr: uint128{0, 0xffff00000000 | uint64(addr[0])<<24 | uint64(addr[1])<<16 | uint64(addr[2])<<8 | uint64(addr[3])},
	}
}

// AddrFrom16 returns the IPv6 address given by the bytes in addr.
// IPv4-mapped IPv6 addresses are represented as IPv4 addresses.
func AddrFrom16(addr [16]byte) Addr {
	return Addr{
		addr: uint128{
			binary.BigEndian.Uint64(addr[:8]),
			binary.BigEndian.Uint64(addr[8:]),
		},
	}
}

// ParseAddr parses s as an IP address, returning the result. The string
// s can be in dotted decimal ("192.0.2.1"), IPv6 ("2001:db8::68"),
// or IPv6 with a scoped addressing zone ("fe80::1cc0:3e8c:119f:c2e1%ens18").
// An explicitly specified zone must be nonempty and is not retained in the returned Addr.
func ParseAddr(s string) (Addr, error) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			return parseIPv4(s)
		case ':':
			return parseIPv6(s)
		case '%':
			// Assume that this was trying to be an IPv6 address with
			// a zone specifier, but the address is missing.
			return Addr{}, parseAddrError{in: s, msg: "missing IPv6 address"}
		}
	}
	return Addr{}, parseAddrError{in: s, msg: "unable to parse IP"}
}

// MustParseAddr calls [ParseAddr](s) and panics on error.
// It is intended for use in tests with hard-coded strings.
func MustParseAddr(s string) Addr {
	ip, err := ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return ip
}

type parseAddrError struct {
	in  string // the string given to ParseAddr
	msg string // an explanation of the parse failure
	at  string // optionally, the unparsed portion of in at which the error occurred.
}

// Error formats an address parsing error and its optional failure location.
func (err parseAddrError) Error() string {
	q := strconv.Quote
	if err.at != "" {
		return "ParseAddr(" + q(err.in) + "): " + err.msg + " (at " + q(err.at) + ")"
	}
	return "ParseAddr(" + q(err.in) + "): " + err.msg
}

// parseIPv4Fields parses four decimal octets within in[off:end] into fields.
func parseIPv4Fields(in string, off, end int, fields []uint8) error {
	var val, pos int
	var digLen int // number of digits in current octet
	s := in[off:end]
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if digLen == 1 && val == 0 {
				return parseAddrError{in: in, msg: "IPv4 field has octet with leading zero"}
			}
			val = val*10 + int(s[i]) - '0'
			digLen++
			if val > 255 {
				return parseAddrError{in: in, msg: "IPv4 field has value >255"}
			}
		} else if s[i] == '.' {
			// .1.2.3
			// 1.2.3.
			// 1..2.3
			if i == 0 || i == len(s)-1 || s[i-1] == '.' {
				return parseAddrError{in: in, msg: "IPv4 field must have at least one digit", at: s[i:]}
			}
			// 1.2.3.4.5
			if pos == 3 {
				return parseAddrError{in: in, msg: "IPv4 address too long"}
			}
			fields[pos] = uint8(val)
			pos++
			val = 0
			digLen = 0
		} else {
			return parseAddrError{in: in, msg: "unexpected character", at: s[i:]}
		}
	}
	if pos < 3 {
		return parseAddrError{in: in, msg: "IPv4 address too short"}
	}
	fields[3] = uint8(val)
	return nil
}

// parseIPv4 parses s as an IPv4 address (in form "192.168.0.1").
func parseIPv4(s string) (ip Addr, err error) {
	var fields [4]uint8
	err = parseIPv4Fields(s, 0, len(s), fields[:])
	if err != nil {
		return Addr{}, err
	}
	return AddrFrom4(fields), nil
}

// parseIPv6 parses s as an IPv6 address (in form "2001:db8::68").
func parseIPv6(in string) (Addr, error) {
	s := in

	// Split off the zone right from the start. Yes it's a second scan
	// of the string, but trying to handle it inline makes a bunch of
	// other inner loop conditionals more expensive, and it ends up
	// being slower.
	zone := ""
	i := strings.IndexByte(s, '%')
	if i != -1 {
		s, zone = s[:i], s[i+1:]
		if zone == "" {
			// Not allowed to have an empty zone if explicitly specified.
			return Addr{}, parseAddrError{in: in, msg: "zone must be a non-empty string"}
		}
	}

	var ip [16]byte
	ellipsis := -1 // position of ellipsis in ip

	// Might have leading ellipsis
	if len(s) >= 2 && s[0] == ':' && s[1] == ':' {
		ellipsis = 0
		s = s[2:]
		// Might be only ellipsis
		if len(s) == 0 {
			return Addr{}, nil
		}
	}

	// Loop, parsing hex numbers followed by colon.
	i = 0
	for i < 16 {
		// Hex number. Similar to parseIPv4, inlining the hex number
		// parsing yields a significant performance increase.
		off := 0
		acc := uint32(0)
		for ; off < len(s); off++ {
			c := s[off]
			if c >= '0' && c <= '9' {
				acc = (acc << 4) + uint32(c-'0')
			} else if c >= 'a' && c <= 'f' {
				acc = (acc << 4) + uint32(c-'a'+10)
			} else if c >= 'A' && c <= 'F' {
				acc = (acc << 4) + uint32(c-'A'+10)
			} else {
				break
			}
			if off > 3 {
				//more than 4 digits in group, fail.
				return Addr{}, parseAddrError{in: in, msg: "each group must have 4 or less digits", at: s}
			}
			if acc > math.MaxUint16 {
				// Overflow, fail.
				return Addr{}, parseAddrError{in: in, msg: "IPv6 field has value >=2^16", at: s}
			}
		}
		if off == 0 {
			// No digits found, fail.
			return Addr{}, parseAddrError{in: in, msg: "each colon-separated field must have at least one digit", at: s}
		}

		// If followed by dot, might be in trailing IPv4.
		if off < len(s) && s[off] == '.' {
			if ellipsis < 0 && i != 12 {
				// Not the right place.
				return Addr{}, parseAddrError{in: in, msg: "embedded IPv4 address must replace the final 2 fields of the address", at: s}
			}
			if i+4 > 16 {
				// Not enough room.
				return Addr{}, parseAddrError{in: in, msg: "too many hex fields to fit an embedded IPv4 at the end of the address", at: s}
			}

			end := len(in)
			if len(zone) > 0 {
				end -= len(zone) + 1
			}
			err := parseIPv4Fields(in, end-len(s), end, ip[i:i+4])
			if err != nil {
				return Addr{}, err
			}
			s = ""
			i += 4
			break
		}

		// Save this 16-bit chunk.
		ip[i] = byte(acc >> 8)
		ip[i+1] = byte(acc)
		i += 2

		// Stop at end of string.
		s = s[off:]
		if len(s) == 0 {
			break
		}

		// Otherwise must be followed by colon and more.
		if s[0] != ':' {
			return Addr{}, parseAddrError{in: in, msg: "unexpected character, want colon", at: s}
		} else if len(s) == 1 {
			return Addr{}, parseAddrError{in: in, msg: "colon must be followed by more characters", at: s}
		}
		s = s[1:]

		// Look for ellipsis.
		if s[0] == ':' {
			if ellipsis >= 0 { // already have one
				return Addr{}, parseAddrError{in: in, msg: "multiple :: in address", at: s}
			}
			ellipsis = i
			s = s[1:]
			if len(s) == 0 { // can be at end
				break
			}
		}
	}

	// Must have used entire string.
	if len(s) != 0 {
		return Addr{}, parseAddrError{in: in, msg: "trailing garbage after address", at: s}
	}

	// If didn't parse enough, expand ellipsis.
	if i < 16 {
		if ellipsis < 0 {
			return Addr{}, parseAddrError{in: in, msg: "address string too short"}
		}
		n := 16 - i
		for j := i - 1; j >= ellipsis; j-- {
			ip[j+n] = ip[j]
		}
		clear(ip[ellipsis : ellipsis+n])
	} else if ellipsis >= 0 {
		// Ellipsis must represent at least one 0 group.
		return Addr{}, parseAddrError{in: in, msg: "the :: must expand to at least one field of zeros"}
	}
	return AddrFrom16(ip), nil
}

// AddrFromSlice parses the 4- or 16-byte byte slice as an IPv4 or IPv6 address.
// Note that a [net.IP] can be passed directly as the []byte argument.
// If slice's length is not 4 or 16, AddrFromSlice returns [Addr]{}, false.
func AddrFromSlice(slice []byte) (ip Addr, ok bool) {
	switch len(slice) {
	case 4:
		return AddrFrom4([4]byte(slice)), true
	case 16:
		return AddrFrom16([16]byte(slice)), true
	}
	return Addr{}, false
}

// v4 returns the i'th byte of ip. If ip is not an IPv4, v4 returns
// unspecified garbage.
func (ip Addr) v4(i uint8) uint8 {
	return uint8(ip.addr.lo >> ((3 - i) * 8))
}

// v6 returns the i'th byte of ip. If ip is an IPv4 address, this
// accesses the IPv4-mapped IPv6 address form of the IP.
func (ip Addr) v6(i uint8) uint8 {
	return uint8(*(ip.addr.halves()[(i/8)%2]) >> ((7 - i%8) * 8))
}

// v6u16 returns the i'th 16-bit word of ip. If ip is an IPv4 address,
// this accesses the IPv4-mapped IPv6 address form of the IP.
func (ip Addr) v6u16(i uint8) uint16 {
	return uint16(*(ip.addr.halves()[(i/4)%2]) >> ((3 - i%4) * 16))
}

// Is4 reports whether ip is an IPv4 address.
func (ip Addr) Is4() bool {
	return ip.addr.hi == 0 && ip.addr.lo>>32 == 0xffff
}

// Is6 reports whether ip is an IPv6 address.
func (ip Addr) Is6() bool {
	return !ip.Is4()
}

// IsLinkLocalUnicast reports whether ip is a link-local unicast address.
func (ip Addr) IsLinkLocalUnicast() bool {
	// Dynamic Configuration of IPv4 Link-Local Addresses
	// https://datatracker.ietf.org/doc/html/rfc3927#section-2.1
	if ip.Is4() {
		return ip.v4(0) == 169 && ip.v4(1) == 254
	}
	// IP Version 6 Addressing Architecture (2.4 Address Type Identification)
	// https://datatracker.ietf.org/doc/html/rfc4291#section-2.4
	if ip.Is6() {
		return ip.v6u16(0)&0xffc0 == 0xfe80
	}
	return false // zero value
}

// IsLoopback reports whether ip is a loopback address.
func (ip Addr) IsLoopback() bool {
	// Requirements for Internet Hosts -- Communication Layers (3.2.1.3 Addressing)
	// https://datatracker.ietf.org/doc/html/rfc1122#section-3.2.1.3
	if ip.Is4() {
		return ip.v4(0) == 127
	}
	// IP Version 6 Addressing Architecture (2.4 Address Type Identification)
	// https://datatracker.ietf.org/doc/html/rfc4291#section-2.4
	if ip.Is6() {
		return ip.addr.hi == 0 && ip.addr.lo == 1
	}
	return false // zero value
}

// IsMulticast reports whether ip is a multicast address.
func (ip Addr) IsMulticast() bool {
	// Host Extensions for IP Multicasting (4. HOST GROUP ADDRESSES)
	// https://datatracker.ietf.org/doc/html/rfc1112#section-4
	if ip.Is4() {
		return ip.v4(0)&0xf0 == 0xe0
	}
	// IP Version 6 Addressing Architecture (2.4 Address Type Identification)
	// https://datatracker.ietf.org/doc/html/rfc4291#section-2.4
	if ip.Is6() {
		return ip.addr.hi>>(64-8) == 0xff // ip.v6(0) == 0xff
	}
	return false // zero value
}

// IsInterfaceLocalMulticast reports whether ip is an IPv6 interface-local
// multicast address.
func (ip Addr) IsInterfaceLocalMulticast() bool {
	// IPv6 Addressing Architecture (2.7.1. Pre-Defined Multicast Addresses)
	// https://datatracker.ietf.org/doc/html/rfc4291#section-2.7.1
	if ip.Is6() {
		return ip.v6u16(0)&0xff0f == 0xff01
	}
	return false // zero value
}

// IsLinkLocalMulticast reports whether ip is a link-local multicast address.
func (ip Addr) IsLinkLocalMulticast() bool {
	// IPv4 Multicast Guidelines (4. Local Network Control Block (224.0.0/24))
	// https://datatracker.ietf.org/doc/html/rfc5771#section-4
	if ip.Is4() {
		return ip.v4(0) == 224 && ip.v4(1) == 0 && ip.v4(2) == 0
	}
	// IPv6 Addressing Architecture (2.7.1. Pre-Defined Multicast Addresses)
	// https://datatracker.ietf.org/doc/html/rfc4291#section-2.7.1
	if ip.Is6() {
		return ip.v6u16(0)&0xff0f == 0xff02
	}
	return false // zero value
}

// IsGlobalUnicast reports whether ip is a global unicast address.
//
// It returns true for IPv6 addresses which fall outside of the current
// IANA-allocated 2000::/3 global unicast space, with the exception of the
// link-local address space. It also returns true even if ip is in the IPv4
// private address space or IPv6 unique local address space.
// It returns false for the zero [Addr], which represents IPv6 unspecified.
//
// For reference, see RFC 1122, RFC 4291, and RFC 4632.
func (ip Addr) IsGlobalUnicast() bool {
	// Match package net's IsGlobalUnicast logic. Notably private IPv4 addresses
	// and ULA IPv6 addresses are still considered "global unicast".
	if ip.Is4() && (ip == IPv4Unspecified() || ip == IPv4Broadcast()) {
		return false
	}

	return ip != IPv6Unspecified() &&
		!ip.IsLoopback() &&
		!ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast()
}

// IsPrivate reports whether ip is a private address, according to RFC 1918
// (IPv4 addresses) and RFC 4193 (IPv6 addresses). That is, it reports whether
// ip is in 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, or fc00::/7. This is the
// same as [net.IP.IsPrivate].
//
// IsPrivate does not describe a security property of addresses,
// and should not be used for access control.
func (ip Addr) IsPrivate() bool {
	// Match the stdlib's IsPrivate logic.
	if ip.Is4() {
		// RFC 1918 allocates 10.0.0.0/8, 172.16.0.0/12, and 192.168.0.0/16 as
		// private IPv4 address subnets.
		return ip.v4(0) == 10 ||
			(ip.v4(0) == 172 && ip.v4(1)&0xf0 == 16) ||
			(ip.v4(0) == 192 && ip.v4(1) == 168)
	}

	if ip.Is6() {
		// RFC 4193 allocates fc00::/7 as the unique local unicast IPv6 address
		// subnet.
		return ip.v6(0)&0xfe == 0xfc
	}

	return false // zero value
}

// IsUnspecified reports whether ip is an unspecified address, either the IPv4
// address "0.0.0.0" or the IPv6 address "::".
//
// The zero [Addr] represents the IPv6 unspecified address.
func (ip Addr) IsUnspecified() bool {
	return ip == IPv4Unspecified() || ip == IPv6Unspecified()
}

// As16 returns the IP address in its 16-byte representation.
// IPv4 addresses are returned as IPv4-mapped IPv6 addresses.
// IPv6 addresses are returned without a zone.
// The ip zero value returns all zeroes.
func (ip Addr) As16() (a16 [16]byte) {
	binary.BigEndian.PutUint64(a16[:8], ip.addr.hi)
	binary.BigEndian.PutUint64(a16[8:], ip.addr.lo)
	return a16
}

// As4 returns an IPv4 or IPv4-in-IPv6 address in its 4-byte representation.
// If ip is an IPv6 address, As4 panics.
// Note that 0.0.0.0 is not the zero Addr.
func (ip Addr) As4() (a4 [4]byte) {
	if !ip.Is4() {
		panic("As4 called on IPv6 address")
	}

	binary.BigEndian.PutUint32(a4[:], uint32(ip.addr.lo))
	return a4
}

// AsSlice returns an IPv4 or IPv6 address in its respective 4-byte or 16-byte representation.
func (ip Addr) AsSlice() []byte {
	if ip.Is4() {
		var ret [4]byte
		binary.BigEndian.PutUint32(ret[:], uint32(ip.addr.lo))
		return ret[:]
	}

	var ret [16]byte
	binary.BigEndian.PutUint64(ret[:8], ip.addr.hi)
	binary.BigEndian.PutUint64(ret[8:], ip.addr.lo)
	return ret[:]
}

// AddrInterface is an IP address and a interface index.
type AddrInterface struct {
	ip      Addr
	ifIndex uint32
}

// Addr returns p's IP address.
func (p AddrInterface) Addr() Addr { return p.ip }

// IfIndex returns p's interface index.
func (p AddrInterface) IfIndex() uint32 { return p.ifIndex }

// AddrInterfaceFrom returns an [AddrInterface] with the provided IP and interface index.
// It does not allocate.
func AddrInterfaceFrom(ip Addr, ifIndex uint32) AddrInterface {
	return AddrInterface{ip: ip, ifIndex: ifIndex}
}

// AddrPort is an IP and a port number.
type AddrPort struct {
	ip   Addr
	port uint16
}

// AddrPortFrom returns an [AddrPort] with the provided IP and port.
// It does not allocate.
func AddrPortFrom(ip Addr, port uint16) AddrPort { return AddrPort{ip: ip, port: port} }

// Addr returns p's IP address.
func (p AddrPort) Addr() Addr { return p.ip }

// Port returns p's port.
func (p AddrPort) Port() uint16 { return p.port }

// AddrInterfacePort is an IP address, interface index, and port number.
type AddrInterfacePort struct {
	ip   AddrInterface
	port uint16
}

// AddrInterfacePortFrom returns an [AddrInterfacePort] with the provided IP and port.
// It does not allocate.
func AddrInterfacePortFrom(ip AddrInterface, port uint16) AddrInterfacePort {
	return AddrInterfacePort{ip: ip, port: port}
}

// AddrInterface returns p's IP address and interface index.
func (p AddrInterfacePort) AddrInterface() AddrInterface { return p.ip }

// Port returns p's port.
func (p AddrInterfacePort) Port() uint16 { return p.port }
