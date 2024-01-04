//go:build !linux
// +build !linux

package kcp

import (
	"net"
	"sync/atomic"

	"github.com/pkg/errors"
)

func (s *UDPSession) readLoop(conn net.PacketConn) {
	buf := make([]byte, mtuLimit)
	var src string
	for {
		if n, addr, err := conn.ReadFrom(buf); err == nil {
			// make sure the packet is from the same source
			if src == "" { // set source address
				src = addr.String()
			} else if addr.String() != src {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
				continue
			}
			s.packetInput(buf[:n])
		} else {
			s.notifyReadError(errors.WithStack(err))
			return
		}
	}
}

func (l *Listener) monitor(conn net.PacketConn) {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := conn.ReadFrom(buf); err == nil {
			l.packetInput(buf[:n], from)
		} else {
			l.notifyReadError(errors.WithStack(err))
			return
		}
	}
}
