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

// isSameUDPAddr compares UDP addresses, including their IPv6 zones.
func isSameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Port != b.Port {
		return false
	}

	if !a.IP.Equal(b.IP) {
		return false
	}

	if a.Zone != b.Zone {
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

		remoteAddr, ok := addr.(*net.UDPAddr)
		if !ok || remoteAddr == nil {
			continue
		}

		// Ensure the packet comes from the same source.
		if !isSameUDPAddr(remoteAddr, s.remoteAddr) {
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

		remoteAddr, ok := raddr.(*net.UDPAddr)
		if !ok || remoteAddr == nil {
			continue
		}

		l.dispatch(buf[:n], libnetip.AddrInterface{}, nil, remoteAddr)
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
// ends reads. Closing a client session, or a Listener once its sessions have
// closed, sets a nil fn, which must remove the receiver.
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

	remoteAddr, ok := raddr.(*net.UDPAddr)
	if !ok || remoteAddr == nil {
		return
	}

	// Ensure the packet comes from the same source.
	if !isSameUDPAddr(remoteAddr, s.remoteAddr) {
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

	remoteAddr, ok := raddr.(*net.UDPAddr)
	if !ok || remoteAddr == nil {
		return
	}

	l.dispatch(b, libnetip.AddrInterface{}, nil, remoteAddr)
}
