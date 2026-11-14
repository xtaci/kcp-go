//go:build !(linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package cmsg

import (
	"syscall"

	"github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
)

const SupportsControlMessage = false

func SetupControlMessage(_ syscall.RawConn) error {
	return nil
}

func ParseControlMessage(_ []byte) (netip.AddrInterface, error) {
	panic("unreachable")
}

func buildControlMessage(laddr netip.AddrInterface) *[]byte {
	panic("unreachable")
}
