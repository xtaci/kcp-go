package batch

import (
	"errors"
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func newPacketConn(conn net.PacketConn) (Conn, error) {
	switch conn.(type) {
	case udpConn:
	case ipConn:
	default:
		return nil, errors.ErrUnsupported
	}

	v4 := false
	switch laddr := conn.LocalAddr().(type) {
	case *net.UDPAddr:
		v4 = laddr.IP.To4() != nil
	case *net.IPAddr:
		v4 = laddr.IP.To4() != nil
	default:
		return nil, errors.ErrUnsupported
	}

	switch v4 {
	case true:
		return ipv4.NewPacketConn(conn), nil
	default:
		return ipv6.NewPacketConn(conn), nil
	}
}
