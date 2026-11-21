//go:build aix && ppc64 && gccgo

package syscall

/*
#include <stdint.h>
int recvmmsg(int, uintptr_t, unsigned int, int, uintptr_t);
int sendmmsg(int, uintptr_t, unsigned int, int);
*/
import "C"
import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func recvmmsg(s int, msg *Mmsghdr, vlen uint32, flags int, timeout *unix.Timespec) (n int, err error) {
	r0, er := C.recvmmsg(C.int(s), C.uintptr_t(uintptr(unsafe.Pointer(msg))), C.uint(vlen), C.int(flags), C.uintptr_t(uintptr(unsafe.Pointer(timeout))))
	n = int(r0)
	if r0 == -1 && er != nil {
		err = er
	}
	return
}

func sendmmsg(s int, msg *Mmsghdr, vlen uint32, flags int) (n int, err error) {
	r0, er := C.sendmmsg(C.int(s), C.uintptr_t(uintptr(unsafe.Pointer(msg))), C.uint(vlen), C.int(flags))
	n = int(r0)
	if r0 == -1 && er != nil {
		err = er
	}
	return
}
