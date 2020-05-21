// +build !linux

package kcp

func (s *UDPTunnel) readLoop() {
	s.defaultReadLoop()
}
