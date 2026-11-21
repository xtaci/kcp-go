//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || windows || zos

package socket

import (
	"net"
	_ "unsafe"
)

//go:linkname parseInetAddr golang.org/x/net/internal/socket.parseInetAddr
func parseInetAddr(b []byte, network string) (net.Addr, error)

//go:linkname marshalInetAddr golang.org/x/net/internal/socket.marshalInetAddr
func marshalInetAddr(a net.Addr, b []byte) int
