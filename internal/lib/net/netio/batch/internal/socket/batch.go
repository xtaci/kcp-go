//go:build aix || darwin || netbsd

package socket

import (
	"net"

	"golang.org/x/net/ipv4"
)

type Message = ipv4.Message

// ReadBatch reads a batch of messages.
//
// The provided flags is a set of platform-dependent flags, such as
// syscall.MSG_PEEK.
//
// On a successful read it returns the number of messages received, up
// to len(ms).
func (c *PacketConn) ReadBatch(ms []Message, flags int) (int, error) {
	if !c.ok() {
		return 0, errInvalidConn
	}
	if len(ms) == 0 {
		return 0, nil
	}

	n, err := c.recvMsgs(ms, flags)
	if err != nil {
		err = &net.OpError{Op: "read", Net: c.PacketConn.LocalAddr().Network(), Source: c.PacketConn.LocalAddr(), Err: err}
	}
	return n, err
}

// WriteBatch writes a batch of messages.
//
// The provided flags is a set of platform-dependent flags, such as
// syscall.MSG_DONTROUTE.
//
// It returns the number of messages written on a successful write.
func (c *PacketConn) WriteBatch(ms []Message, flags int) (int, error) {
	if !c.ok() {
		return 0, errInvalidConn
	}
	if len(ms) == 0 {
		return 0, nil
	}

	n, err := c.sendMsgs(ms, flags)
	if err != nil {
		err = &net.OpError{Op: "write", Net: c.PacketConn.LocalAddr().Network(), Source: c.PacketConn.LocalAddr(), Err: err}
	}
	return n, err
}
