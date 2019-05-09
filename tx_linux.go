// +build linux

package kcp

import (
	"net"
	"sync/atomic"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func (s *UDPSession) txLoop() {
	addr, _ := net.ResolveUDPAddr("udp", s.conn.LocalAddr().String())
	var conn batchConn

	if addr.IP.To4() != nil {
		conn = ipv4.NewPacketConn(s.conn)
	} else {
		conn = ipv6.NewPacketConn(s.conn)
	}

	for {
		select {
		case txqueue := <-s.chTxQueue:
			if len(txqueue) > 0 {
				nbytes := 0
				vec := txqueue
				for len(vec) > 0 {
					if n, err := conn.WriteBatch(vec, 0); err == nil {
						vec = vec[n:]
					} else {
						s.notifyWriteError(err)
						return
					}
				}

				for k := range txqueue {
					nbytes += len(txqueue[k].Buffers[0])
					xmitBuf.Put(txqueue[k].Buffers[0])
				}

				atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(len(txqueue)))
				atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
			}
		case <-s.die:
			return
		}
	}
}

func (l *Listener) txLoop() {
	addr, _ := net.ResolveUDPAddr("udp", l.conn.LocalAddr().String())
	var conn batchConn

	if addr.IP.To4() != nil {
		conn = ipv4.NewPacketConn(l.conn)
	} else {
		conn = ipv6.NewPacketConn(l.conn)
	}

	for {
		select {
		case txqueue := <-l.chTxQueue:
			if len(txqueue) > 0 {
				nbytes := 0
				vec := txqueue
				for len(vec) > 0 {
					if n, err := conn.WriteBatch(vec, 0); err == nil {
						vec = vec[n:]
					} else {
						l.socketError.Store(err)
						l.Close()
						return
					}
				}

				for k := range txqueue {
					nbytes += len(txqueue[k].Buffers[0])
					xmitBuf.Put(txqueue[k].Buffers[0])
				}

				atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(len(txqueue)))
				atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
			}
		case <-l.die:
			return
		}
	}
}
