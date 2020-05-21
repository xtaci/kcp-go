package kcp

import (
	"sync/atomic"

	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
)

func (l *UDPTunnel) defaultReadLoop() {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := l.conn.ReadFrom(buf); err == nil {
			if n >= gouuid.Size+IKCP_OVERHEAD {
				l.Input(buf[:n], from)
			} else {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
			}
		} else {
			l.notifyReadError(errors.WithStack(err))
			return
		}
	}
}
