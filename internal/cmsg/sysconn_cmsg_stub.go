//go:build !(linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package cmsg

import (
	"syscall"

	"github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
)

const SupportsControlMessage = false

// SetupControlMessage is a no-op on platforms without control-message support.
func SetupControlMessage(_ syscall.RawConn) error {
	return nil
}

// ParseControlMessage must not be called on platforms without control-message support.
func ParseControlMessage(_ []byte) (netip.AddrInterface, error) {
	panic("unreachable")
}

// buildControlMessage must not be called on platforms without control-message support.
func buildControlMessage(laddr netip.AddrInterface) *[]byte {
	panic("unreachable")
}
