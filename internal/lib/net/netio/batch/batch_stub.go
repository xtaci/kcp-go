//go:build !(linux || netbsd || aix || (darwin && experimental_darwin_mmsg))

package batch

import (
	"errors"
	"net"
)

func newPacketConn(conn net.PacketConn) (Conn, error) {
	return nil, errors.ErrUnsupported
}
