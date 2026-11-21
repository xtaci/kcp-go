//go:build aix || netbsd || (darwin && experimental_darwin_mmsg)

package batch

import (
	"errors"
	"net"

	"github.com/xtaci/kcp-go/v5/internal/lib/net/netio/batch/internal/socket"
)

func newPacketConn(conn net.PacketConn) (Conn, error) {
	switch conn.(type) {
	case udpConn:
	default:
		return nil, errors.ErrUnsupported
	}

	return socket.NewPacketConn(conn), nil
}
