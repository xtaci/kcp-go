// +build linux

package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) tx(txqueue []ipv4.Message) {
	nbytes := 0
	npkts := 0
	for len(txqueue) > 0 {
		if n, err := s.xconn.WriteBatch(txqueue, 0); err == nil {
			for k := range txqueue[:n] {
				nbytes += len(txqueue[k].Buffers[0])
				xmitBuf.Put(txqueue[k].Buffers[0])
			}
			npkts += n
			txqueue = txqueue[n:]
		} else {
			s.notifyWriteError(errors.WithStack(err))
			break
		}
	}

	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}
