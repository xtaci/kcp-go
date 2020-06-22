package kcp

import (
	"sync/atomic"

	gouuid "github.com/satori/go.uuid"
)

func (t *UDPTunnel) defaultReadLoop() {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := t.conn.ReadFrom(buf); err == nil {
			if n >= gouuid.Size+IKCP_OVERHEAD {
				t.input(buf[:n], from)
			} else {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
			}
		} else {
			t.notifyReadError(err)
		}
	}
}
