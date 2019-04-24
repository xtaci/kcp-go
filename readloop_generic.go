// +build !linux

package kcp

import (
	"sync/atomic"
)

func (s *UDPSession) readLoop() {
	buf := make([]byte, mtuLimit)
	var src string
	for {
		if n, addr, err := s.conn.ReadFrom(buf); err == nil {
			// make sure the packet is from the same source
			if src == "" { // set source address
				src = addr.String()
			} else if addr.String() != src {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
				continue
			}

			if n >= s.headerSize+IKCP_OVERHEAD {
				s.packetInput(buf[:n])
			} else {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
			}
		} else {
			s.chReadError <- err
			return
		}
	}
}

func (l *Listener) monitor() {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := l.conn.ReadFrom(buf); err == nil {
			if n >= l.headerSize+IKCP_OVERHEAD {
				l.packetInput(buf[:n], from)
			} else {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
			}
		} else {
			return
		}
	}
}
