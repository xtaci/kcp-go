package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
)

func (s *UDPTunnel) defaultReadLoop() {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := s.conn.ReadFrom(buf); err == nil {
			if n >= gouuid.Size+IKCP_OVERHEAD {
				s.Input(buf[:n], from)
			} else {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
			}
		} else {
			s.notifyReadError(errors.WithStack(err))
			return
		}
	}
}
