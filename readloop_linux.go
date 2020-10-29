// +build linux

package kcp

import (
	"net"
	"os"
	"sync/atomic"

	gouuid "github.com/satori/go.uuid"
	"golang.org/x/net/ipv4"
)

// monitor incoming data for all connections of server
func (t *UDPTunnel) readLoop() {
	// default version
	if t.xconn == nil {
		t.defaultReadLoop()
		return
	}

	// x/net version
	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{xmitBuf.Get().([]byte)[:mtuLimit]}
	}

	for {
		select {
		case <-t.die:
			return
		default:
		}

		if count, err := t.xconn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				if msg.N >= gouuid.Size+IKCP_OVERHEAD {
					t.input(msg.Buffers[0][:msg.N], msg.Addr)
					msg.Buffers[0] = xmitBuf.Get().([]byte)[:mtuLimit]
				} else {
					atomic.AddUint64(&DefaultSnmp.InErrs, 1)
				}
			}
		} else {
			// compatibility issue:
			// for linux kernel<=2.6.32, support for sendmmsg is not available
			// an error of type os.SyscallError will be returned
			if operr, ok := err.(*net.OpError); ok {
				if se, ok := operr.Err.(*os.SyscallError); ok {
					if se.Syscall == "recvmmsg" {
						t.defaultReadLoop()
						return
					}
				}
			}
			t.notifyReadError(err)
		}
	}
}
