// +build !linux

package kcp

func (s *UDPTunnel) writeLoop() {
	s.defaultWriteLoop()
}
