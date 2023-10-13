package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) readLoop() {
	// default version
	if s.xconn == nil {
		s.defaultReadLoop()
		return
	}
	s.batchReadLoop()
}

func (l *Listener) monitor() {
	xconn := toBatchConn(l.conn)

	// default version
	if xconn == nil {
		l.defaultMonitor()
		return
	}
	l.batchMonitor(xconn)
}

func (s *UDPSession) defaultReadLoop() {
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
			s.packetInput(buf[:n])
		} else {
			s.notifyReadError(errors.WithStack(err))
			return
		}
	}
}

func (l *Listener) defaultMonitor() {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := l.conn.ReadFrom(buf); err == nil {
			l.packetInput(buf[:n], from)
		} else {
			l.notifyReadError(errors.WithStack(err))
			return
		}
	}
}

func (s *UDPSession) batchReadLoop() {
	// x/net version
	var src string
	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	for {
		if count, err := s.xconn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				// make sure the packet is from the same source
				if msg.Addr.String() != src {
					if len(src) == 0 { // set source address if nil
						src = msg.Addr.String()
					} else {
						atomic.AddUint64(&DefaultSnmp.InErrs, 1)
						continue
					}
				}

				// source and size has validated
				s.packetInput(msg.Buffers[0][:msg.N])
			}
		} else {
			if readBatchUnavailable(s.xconn, err) {
				s.defaultReadLoop()
				return
			}
			s.notifyReadError(errors.WithStack(err))
			return
		}
	}
}

func (l *Listener) batchMonitor(xconn batchConn) {
	// x/net version
	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	for {
		if count, err := xconn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				l.packetInput(msg.Buffers[0][:msg.N], msg.Addr)
			}
		} else {
			if readBatchUnavailable(xconn, err) {
				l.defaultMonitor()
				return
			}
			l.notifyReadError(errors.WithStack(err))
			return
		}
	}
}
