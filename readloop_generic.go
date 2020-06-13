// +build !linux

package kcp

func (t *UDPTunnel) readLoop() {
	t.defaultReadLoop()
}
