package socket

import (
	"errors"
	"net"
	"syscall"
)

// A Conn represents a raw connection.
type Conn struct {
	network string
	c       syscall.RawConn
}

// tcpConn is an interface implemented by net.TCPConn.
// It can be used for interface assertions to check if a net.Conn is a TCP connection.
type tcpConn interface {
	SyscallConn() (syscall.RawConn, error)
	SetLinger(int) error
}

var _ tcpConn = (*net.TCPConn)(nil)

// udpConn is an interface implemented by net.UDPConn.
// It can be used for interface assertions to check if a net.Conn is a UDP connection.
type udpConn interface {
	SyscallConn() (syscall.RawConn, error)
	ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error)
}

var _ udpConn = (*net.UDPConn)(nil)

// ipConn is an interface implemented by net.IPConn.
// It can be used for interface assertions to check if a net.Conn is an IP connection.
type ipConn interface {
	SyscallConn() (syscall.RawConn, error)
	ReadMsgIP(b, oob []byte) (n, oobn, flags int, addr *net.IPAddr, err error)
}

var _ ipConn = (*net.IPConn)(nil)

// NewConn returns a new raw connection.
func NewConn(c net.Conn) (*Conn, error) {
	var err error
	var cc Conn
	switch c := c.(type) {
	case tcpConn:
		cc.network = "tcp"
		cc.c, err = c.SyscallConn()
	case udpConn:
		cc.network = "udp"
		cc.c, err = c.SyscallConn()
	case ipConn:
		cc.network = "ip"
		cc.c, err = c.SyscallConn()
	default:
		return nil, errors.New("unknown connection type")
	}
	if err != nil {
		return nil, err
	}
	return &cc, nil
}
