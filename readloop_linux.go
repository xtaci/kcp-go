// +build linux

package kcp

import (
	"net"
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// the read loop for a client session
func (s *UDPSession) readLoop() {
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
				if src == "" { // set source address if nil
					src = msg.Addr.String()
				} else if msg.Addr.String() != src {
					atomic.AddUint64(&DefaultSnmp.InErrs, 1)
					continue
				}

				if msg.N < s.headerSize+IKCP_OVERHEAD {
					atomic.AddUint64(&DefaultSnmp.InErrs, 1)
					continue
				}

				// source and size has validated
				s.packetInput(msg.Buffers[0][:msg.N])
			}
		} else {
			s.notifyReadError(errors.WithStack(err))
			return
		}
	}
}

// monitor incoming data for all connections of server
func (l *Listener) monitor() {
	addr, _ := net.ResolveUDPAddr("udp", l.conn.LocalAddr().String())
	var xconn batchConn
	if addr.IP.To4() != nil {
		xconn = ipv4.NewPacketConn(l.conn)
	} else {
		xconn = ipv6.NewPacketConn(l.conn)
	}

	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	for {
		if count, err := xconn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				if msg.N >= l.headerSize+IKCP_OVERHEAD {
					l.packetInput(msg.Buffers[0][:msg.N], msg.Addr)
				} else {
					atomic.AddUint64(&DefaultSnmp.InErrs, 1)
				}
			}
		} else {
			l.notifyReadError(errors.WithStack(err))
			return
		}
	}
}
