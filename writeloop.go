package kcp

import (
	"sync/atomic"

	"golang.org/x/net/ipv4"
)

func (t *UDPTunnel) writeSingle(msgs []ipv4.Message) {
	nbytes := 0
	npkts := 0
	for k := range msgs {
		if n, err := t.conn.WriteTo(msgs[k].Buffers[0], msgs[k].Addr); err == nil {
			nbytes += n
			npkts++
		} else {
			t.notifyWriteError(err)
		}
	}

	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}

func (t *UDPTunnel) defaultWriteLoop() {
	var msgss [][]ipv4.Message
	for {
		select {
		case <-t.die:
			return
		case <-t.chFlush:
		}

		t.popMsgss(&msgss)
		for _, msgs := range msgss {
			t.writeSingle(msgs)
		}
		t.releaseMsgss(msgss)
		msgss = msgss[:0]
	}
}
