// +build linux

package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) tx(txqueue []ipv4.Message) {
	nbytes := 0
	vec := txqueue
	for len(vec) > 0 {
		if n, err := s.bconn.WriteBatch(vec, 0); err == nil {
			vec = vec[n:]
		} else {
			s.socketError.Store(errors.WithStack(err))
			s.Close()
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
