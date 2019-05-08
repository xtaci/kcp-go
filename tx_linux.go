// +build linux

package kcp

import (
	"net"
	"sync/atomic"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type batchConn interface {
	WriteBatch(ms []ipv4.Message, flags int) (int, error)
}

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
						break
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
