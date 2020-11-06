// +build linux

package kcp

import (
	"net"
	"os"
	"sync/atomic"

	"golang.org/x/net/ipv4"
)

func (t *UDPTunnel) writeBatch(msgs []ipv4.Message) {
	// x/net version
	nbytes := 0
	npkts := 0

	for len(msgs) > 0 {
		if n, err := t.xconn.WriteBatch(msgs, 0); err == nil {
			for k := range msgs[:n] {
				nbytes += len(msgs[k].Buffers[0])
			}
			npkts += n
			msgs = msgs[n:]
		} else {
			// compatibility issue:
			// for linux kernel<=2.6.32, support for sendmmsg is not available
			// an error of type os.SyscallError will be returned
			if operr, ok := err.(*net.OpError); ok {
				if se, ok := operr.Err.(*os.SyscallError); ok {
					if se.Syscall == "sendmmsg" {
						t.xconnWriteError = se
						t.writeSingle(msgs)
						return
					}
				}
			}
			t.notifyWriteError(err)
		}
	}

	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}

func (t *UDPTunnel) writeLoop() {
	// default version
	if t.xconn == nil || t.xconnWriteError != nil {
		t.defaultWriteLoop()
		return
	}

	var msgss [][]ipv4.Message
	for {
		select {
		case <-t.die:
			return
		case <-t.chFlush:
		}

		t.popMsgss(&msgss)
		for _, msgs := range msgss {
			t.writeBatch(msgs)
		}
		t.releaseMsgss(msgss)
		msgss = msgss[:0]
	}
}
