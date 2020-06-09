package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPTunnel) writeSingle(msgs []ipv4.Message) {
	Logf(DEBUG, "UDPTunnel::writeSingle msgs:%v", len(msgs))

	nbytes := 0
	npkts := 0
	for k := range msgs {
		if n, err := s.conn.WriteTo(msgs[k].Buffers[0], msgs[k].Addr); err == nil {
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
		select {
		case <-s.die:
			return
		case <-s.chFlush:
		}

		msgss := s.popMsgss()
		for _, msgs := range msgss {
			s.writeSingle(msgs)
		}
		s.releaseMsgss(msgss)
	}
}
