package syscall

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func recvmmsg(s int, mmsg *Mmsghdr, vlen uint32, flags uint32, timeout *unix.Timespec) (n int, err error) {
	r0, _, e1 := unix.Syscall6(unix.SYS_RECVMMSG, uintptr(s), uintptr(unsafe.Pointer(mmsg)), uintptr(vlen), uintptr(flags), uintptr(unsafe.Pointer(timeout)), 0)
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

func sendmmsg(s int, msg *Mmsghdr, vlen uint32, flags uint32) (n int, err error) {
	r0, _, e1 := unix.Syscall6(unix.SYS_SENDMMSG, uintptr(s), uintptr(unsafe.Pointer(msg)), uintptr(vlen), uintptr(flags), 0, 0)
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}
