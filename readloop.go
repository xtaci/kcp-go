// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
// Copyright (c) 2026 Coia Prant
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package kcp

import (
	"net"

	"github.com/pkg/errors"
	"github.com/xtaci/kcp-go/v5/internal/cmsg"
	libnetip "github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
)

// isUnicast rejects unspecified, multicast, and limited broadcast destinations.
func isUnicast(addr libnetip.Addr) bool {
	if addr.IsUnspecified() {
		return false
	}

	if addr.IsMulticast() {
		return false
	}

	if addr == libnetip.IPv4Broadcast() {
		return false
	}

	return true
}

// isValidAddr reports whether addr is non-nil and, for UDP addresses,
// whether the underlying *net.UDPAddr is non-nil.
func isValidAddr(addr net.Addr) bool {
	if addr == nil {
		return false
	}

	if udpAddr, ok := addr.(*net.UDPAddr); ok && udpAddr == nil {
		return false
	}

	return true
}

// isSameAddr compares UDP addresses by IP, port, and zone. Non-UDP
// addresses are compared by String(); UDP and non-UDP addresses never match.
func isSameAddr(addr1, addr2 net.Addr) bool {
	if addr1 == nil || addr2 == nil {
		return false
	}

	udpAddr1, ok1 := addr1.(*net.UDPAddr)
	udpAddr2, ok2 := addr2.(*net.UDPAddr)

	if (ok1 && udpAddr1 == nil) || (ok2 && udpAddr2 == nil) || ok1 != ok2 {
		return false
	}

	if !ok1 {
		return addr1.String() == addr2.String()
	}

	if udpAddr1.Port != udpAddr2.Port {
		return false
	}

	if !udpAddr1.IP.Equal(udpAddr2.IP) {
		return false
	}

	if udpAddr1.Zone != udpAddr2.Zone {
		return false
	}

	return true
}

// readLoopGeneric is the standard procedure for reading from a connection
func (s *UDPSession) readLoopGeneric() {
	buf := make([]byte, maxPacketSize)

	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			s.notifyReadError(errors.WithStack(err))
			return
		}

		if s.isClosed() {
			return
		}

		if !isValidAddr(addr) {
			continue
		}

		// Ensure the packet comes from the same source.
		if !isSameAddr(addr, s.remoteAddr) {
			DefaultSnmp.increaseInErrs()
			continue
		}

		s.dispatch(buf[:n])
	}
}

// readLoopGeneric is the standard procedure for reading and accepting connections on a listener
func (l *Listener) readLoopGeneric() {
	buf := make([]byte, maxPacketSize)
	for {
		n, raddr, err := l.conn.ReadFrom(buf)
		if err != nil {
			l.notifyReadError(errors.WithStack(err))
			return
		}

		if !isValidAddr(raddr) {
			continue
		}

		l.dispatch(buf[:n], libnetip.AddrInterface{}, nil, raddr)
	}
}

// readLoopUnspecifiedGeneric receives packets and their local destination addresses.
func (l *Listener) readLoopUnspecifiedGeneric() {
	udpConn := l.conn.(udpConn)

	buf := make([]byte, maxPacketSize)
	oob := make([]byte, cmsgPacketSize)
	for {
		n, nn, _, remoteAddr, err := udpConn.ReadMsgUDP(buf, oob)
		if err != nil {
			l.notifyReadError(errors.WithStack(err))
			return
		}

		// The destination is required to select the reply source and session key.
		laddr, err := cmsg.ParseControlMessage(oob[:nn])
		if err != nil {
			continue
		}

		if !isUnicast(laddr.Addr()) {
			continue
		}

		localAddr := cmsg.ResolveUDPAddr(laddr, l.udpPort)
		l.dispatch(buf[:n], laddr, localAddr, remoteAddr)
	}
}

// PacketPusher is a net.PacketConn that can hand each received packet to a
// callback instead of being read. A client session or Listener on such a
// conn runs no read goroutine and processes each packet inside the callback,
// OOB handlers included, so a slow handler delays every conn the pusher
// serves.
//
// The pusher must not call fn concurrently, just as one goroutine reads a
// conn. fn may modify b but must not keep it after returning. A non-nil err
// ends reads. Closing a client session or Listener sets a nil fn, which must
// remove the receiver. Closing a Listener also closes its accepted sessions.
//
// When err is nil, raddr must be a valid, non-nil net.Addr. UDP addresses are
// matched by IP, port, and zone; other address types are matched by String().
// For custom addresses, String() must return a stable value that uniquely
// identifies the peer.
//
// SetPacketReceiver is called with internal locks held. Implementations must
// not call back into kcp from inside SetPacketReceiver — no calls into the
// owning Listener or session, and no acquisition of any lock reachable from
// them. Doing so will deadlock.
type PacketPusher interface {
	SetPacketReceiver(fn func(b []byte, raddr net.Addr, err error))
}

// pushedPacket is the PacketPusher callback of a client session.
func (s *UDPSession) pushedPacket(b []byte, raddr net.Addr, err error) {
	if err != nil {
		s.notifyReadError(errors.WithStack(err))
		return
	}
	if s.isClosed() {
		return
	}

	if !isValidAddr(raddr) {
		return
	}

	// Ensure the packet comes from the same source.
	if !isSameAddr(raddr, s.remoteAddr) {
		DefaultSnmp.increaseInErrs()
		return
	}

	s.dispatch(b)
}

// pushedPacket is the PacketPusher callback of a Listener.
func (l *Listener) pushedPacket(b []byte, raddr net.Addr, err error) {
	if err != nil {
		l.notifyReadError(errors.WithStack(err))
		return
	}

	if !isValidAddr(raddr) {
		return
	}

	l.dispatch(b, libnetip.AddrInterface{}, nil, raddr)
}
