//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package syscall

import (
	_ "unsafe"

	"golang.org/x/sys/unix"
)

//go:linkname errnoErr golang.org/x/sys/unix.errnoErr
func errnoErr(e unix.Errno) error
