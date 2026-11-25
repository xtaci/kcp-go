package socket

import "net"

// A PacketConn represents a packet network endpoint that uses the
// IPv4 transport. It is used to control several IP-level socket
// options including multicasting. It also provides datagram based
// network I/O methods specific to the IPv4 and higher layer protocols
// such as UDP.
type PacketConn struct {
	net.PacketConn
	*Conn
}

func (c *PacketConn) ok() bool {
	return c != nil && c.PacketConn != nil && c.Conn != nil
}

func NewPacketConn(c net.PacketConn) *PacketConn {
	cc, _ := NewConn(c.(net.Conn))
	return &PacketConn{
		PacketConn: c,
		Conn:       cc,
	}
}
