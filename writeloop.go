package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPTunnel) writeSingle(txqueue []ipv4.Message) {
	nbytes := 0
	npkts := 0
	for k := range txqueue {
		if n, err := s.conn.WriteTo(txqueue[k].Buffers[0], txqueue[k].Addr); err == nil {
			nbytes += n
			npkts++
		} else {
			s.notifyWriteError(errors.WithStack(err))
			break
		}
	}

	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}

func (s *UDPTunnel) defaultWriteLoop() {
	for {
		if s.IsClosed() {
			return
		}

		s.mu.Lock()
		txqueues := s.txqueues
		// s.txqueues = s.txqueues[:0]
		s.txqueues = make([][]ipv4.Message, 0)
		s.mu.Unlock()
		for _, txqueue := range txqueues {
			s.writeSingle(txqueue)
		}
		s.ReleaseTX(txqueues)
	}
}
