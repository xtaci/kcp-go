package syscall

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func recvmmsg(s int, msg *Mmsghdr, vlen uint32, flags int, timeout *unix.Timespec) (n int, err error) {
	r0, e1 := callrecvmmsg(s, uintptr(unsafe.Pointer(msg)), vlen, flags, uintptr(unsafe.Pointer(timeout)))
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

func sendmmsg(s int, msg *Mmsghdr, vlen uint32, flags int) (n int, err error) {
	r0, e1 := callsendmmsg(s, uintptr(unsafe.Pointer(msg)), vlen, flags)
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}
