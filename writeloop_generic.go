// +build !linux

package kcp

func (t *UDPTunnel) writeLoop() {
	t.defaultWriteLoop()
}
