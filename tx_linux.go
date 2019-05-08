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
						vec := msgs
						for len(vec) > 0 {
							if n, err := conn.WriteBatch(vec, 0); err == nil {
								vec = vec[n:]
							} else {
								s.notifyWriteError(err)
								break
							}
						}
					}
				}

				if remain := len(txqueue) % batchSize; remain > 0 {
					vec := msgs[:remain]
					for len(vec) > 0 {
						if n, err := conn.WriteBatch(vec, 0); err == nil {
							vec = vec[n:]
						} else {
							s.notifyWriteError(err)
							break
						}
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
						vec := msgs
						for len(vec) > 0 {
							if n, err := conn.WriteBatch(vec, 0); err == nil {
								vec = vec[n:]
							} else {
								s.notifyWriteError(err)
								break
							}
						}
					}
				}

				if remain := len(txqueue) % batchSize; remain > 0 {
					vec := msgs[:remain]
					for len(vec) > 0 {
						if n, err := conn.WriteBatch(vec, 0); err == nil {
							vec = vec[n:]
						} else {
							s.notifyWriteError(err)
							break
						}
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
