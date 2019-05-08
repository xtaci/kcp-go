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
	if addr.IP.To4() != nil {
		s.txLoopIPv4()
	} else {
		s.txLoopIPv6()
	}
}

func (s *UDPSession) txLoopIPv4() {
	conn := ipv4.NewPacketConn(s.conn)
	msgs := make([]ipv4.Message, batchSize)

	for {
		select {
		case txqueue := <-s.chTxQueue:
			if len(txqueue) > 0 {
				nbytes := 0

				for k := range txqueue {
					idx := k % batchSize
					msgs[idx].Addr = s.remote
					msgs[idx].Buffers = [][]byte{txqueue[k]}
					nbytes += len(txqueue[k])

					if (k+1)%batchSize == 0 {
						if _, err := conn.WriteBatch(msgs, 0); err != nil {
							s.notifyWriteError(err)
						}
					}
				}

				if remain := len(txqueue) % batchSize; remain > 0 {
					if _, err := conn.WriteBatch(msgs[:remain], 0); err != nil {
						s.notifyWriteError(err)
					}
				}

				for k := range txqueue {
					xmitBuf.Put(txqueue[k])
				}

				atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(len(txqueue)))
				atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
			}
		case <-s.die:
			return
		}
	}
}

func (s *UDPSession) txLoopIPv6() {
	conn := ipv6.NewPacketConn(s.conn)
	msgs := make([]ipv6.Message, batchSize)

	for {
		select {
		case txqueue := <-s.chTxQueue:
			if len(txqueue) > 0 {
				nbytes := 0

				for k := range txqueue {
					idx := k % batchSize
					msgs[idx].Addr = s.remote
					msgs[idx].Buffers = [][]byte{txqueue[k]}
					nbytes += len(txqueue[k])

					if (k+1)%batchSize == 0 {
						if _, err := conn.WriteBatch(msgs, 0); err != nil {
							s.notifyWriteError(err)
						}
					}
				}

				if remain := len(txqueue) % batchSize; remain > 0 {
					if _, err := conn.WriteBatch(msgs[:remain], 0); err != nil {
						s.notifyWriteError(err)
					}
				}

				for k := range txqueue {
					xmitBuf.Put(txqueue[k])
				}

				atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(len(txqueue)))
				atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
			}
		case <-s.die:
			return
		}
	}
}
