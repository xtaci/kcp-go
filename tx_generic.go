// +build !linux

package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) tx(txqueue []ipv4.Message) {
	nbytes := 0
	for k := range txqueue {
		if n, err := s.conn.WriteTo(txqueue[k].Buffers[0], txqueue[k].Addr); err == nil {
			nbytes += n
			xmitBuf.Put(txqueue[k].Buffers[0])
		} else {
			s.socketError.Store(errors.WithStack(err))
			s.Close()
			return
		}
	}
	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(len(txqueue)))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}
