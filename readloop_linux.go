// +build linux

package kcp

import (
	"net"
	"sync/atomic"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// ReadBatch() message size
	batchSize = 16
)

// the read loop for a client session
func (s *UDPSession) readLoop() {
	addr, _ := net.ResolveUDPAddr("udp", s.conn.LocalAddr().String())
	if addr.IP.To4() != nil {
		s.readLoopIPv4()
	} else {
		s.readLoopIPv6()
	}
}

func (s *UDPSession) readLoopIPv6() {
	var src string
	msgs := make([]ipv6.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	conn := ipv6.NewPacketConn(s.conn)

	for {
		if count, err := conn.ReadBatch(msgs, 0); err == nil {
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
			s.chReadError <- err
			return
		}
	}
}

func (s *UDPSession) readLoopIPv4() {
	var src string
	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	conn := ipv4.NewPacketConn(s.conn)
	for {
		if count, err := conn.ReadBatch(msgs, 0); err == nil {
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
			s.chReadError <- err
			return
		}
	}
}

// monitor incoming data for all connections of server
func (l *Listener) monitor() {
	addr, _ := net.ResolveUDPAddr("udp", l.conn.LocalAddr().String())
	if addr.IP.To4() != nil {
		l.monitorIPv4()
	} else {
		l.monitorIPv6()
	}
}

func (l *Listener) monitorIPv4() {
	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	conn := ipv4.NewPacketConn(l.conn)
	for {
		if count, err := conn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				if msg.N >= l.headerSize+IKCP_OVERHEAD {
					l.packetInput(msg.Buffers[0][:msg.N], msg.Addr)
				} else {
					atomic.AddUint64(&DefaultSnmp.InErrs, 1)
				}
			}
		} else {
			return
		}
	}
}

func (l *Listener) monitorIPv6() {
	msgs := make([]ipv6.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	conn := ipv4.NewPacketConn(l.conn)
	for {
		if count, err := conn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				if msg.N >= l.headerSize+IKCP_OVERHEAD {
					l.packetInput(msg.Buffers[0][:msg.N], msg.Addr)
				} else {
					atomic.AddUint64(&DefaultSnmp.InErrs, 1)
				}
			}
		} else {
			return
		}
	}
}
