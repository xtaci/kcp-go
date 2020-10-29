package kcp

import (
	"sync/atomic"

	gouuid "github.com/satori/go.uuid"
)

func (t *UDPTunnel) defaultReadLoop() {
	buf := xmitBuf.Get().([]byte)[:mtuLimit]
	for {
		select {
		case <-t.die:
			return
		default:
		}

		if n, from, err := t.conn.ReadFrom(buf); err == nil {
			if n >= gouuid.Size+IKCP_OVERHEAD {
				t.input(buf[:n], from)
				buf = xmitBuf.Get().([]byte)[:mtuLimit]
			} else {
				atomic.AddUint64(&DefaultSnmp.InErrs, 1)
			}
		} else {
			t.notifyReadError(err)
		}
	}
}
