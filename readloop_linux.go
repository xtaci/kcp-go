// +build linux

package kcp

import (
	"net"
	"os"
	"sync/atomic"

	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
	"golang.org/x/net/ipv4"
)

// monitor incoming data for all connections of server
func (s *UDPTunnel) readLoop() {
	// default version
	if s.xconn == nil {
		s.defaultReadLoop()
		return
	}

	// x/net version
	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, mtuLimit)}
	}

	for {
		if count, err := s.xconn.ReadBatch(msgs, 0); err == nil {
			for i := 0; i < count; i++ {
				msg := &msgs[i]
				if msg.N >= gouuid.Size+IKCP_OVERHEAD {
					s.input(msg.Buffers[0][:msg.N], msg.Addr)
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
						s.defaultReadLoop()
						return
					}
				}
			}
			s.notifyReadError(errors.WithStack(err))
		}
	}
}
