package batch

import (
	_ "errors"
	"net"
	"syscall"

	"golang.org/x/net/ipv4"
)

type (
	Conn interface {
		ReadBatch(ms []ipv4.Message, flags int) (int, error)
		WriteBatch(ms []ipv4.Message, flags int) (int, error)
	}

	// udpConn is an interface implemented by net.UDPConn.
	// It can be used for interface assertions to check if a net.Conn is a UDP connection.
	udpConn interface {
		SyscallConn() (syscall.RawConn, error)
		ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error)
	}

	// ipConn is an interface implemented by net.IPConn.
	// It can be used for interface assertions to check if a net.Conn is an IP connection.
	ipConn interface {
		SyscallConn() (syscall.RawConn, error)
		ReadMsgIP(b, oob []byte) (n, oobn, flags int, addr *net.IPAddr, err error)
	}
)

// NewPacketConn returns a batch-capable Conn backed by conn.
//
// OS Compatibility:
//
//   - Linux:
//
//     Uses recvmmsg/sendmmsg and is available by default.
//     Supports *net.UDPConn and *net.IPConn.
//     When conn is *net.IPConn, ReadBatch does not strip the IPv4 header
//     (unlike ReadFrom), so each Message.Buffers must be large enough to
//     hold the IP header and its options in addition to the payload.
//
//   - AIX:
//
//     Uses recvmmsg/sendmmsg and is available by default.
//     Supports *net.UDPConn.
//
//   - NetBSD:
//
//     Uses recvmmsg/sendmmsg and is available by default.
//     Supports *net.UDPConn.
//
//   - Darwin:
//
//     Uses the private XNU syscalls recvmsg_x/sendmsg_x.
//
//     Available only when built with the experimental_darwin_mmsg build tag.
//     Supports *net.UDPConn.
//
//     This path is disabled by default because these are private,
//     unsupported APIs; using them can cause App Store review rejection.
//
//     Without the tag, [NewPacketConn] returns [errors.ErrUnsupported] on Darwin.
//
//   - Other platforms:
//
//     Unavailable. [NewPacketConn] returns [errors.ErrUnsupported].
//
// If conn is not one of the supported connection types listed above for the
// target platform, [NewPacketConn] returns [errors.ErrUnsupported].
func NewPacketConn(conn net.PacketConn) (Conn, error) {
	if batchConn, ok := conn.(Conn); ok {
		return batchConn, nil
	}

	return newPacketConn(conn)
}
