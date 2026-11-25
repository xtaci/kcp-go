//go:build aix && ppc64 && gccgo

package syscall

/*
#include <stdint.h>
int recvmmsg(int, uintptr_t, unsigned int, int, uintptr_t);
int sendmmsg(int, uintptr_t, unsigned int, int);
*/
import "C"
import (
	"syscall"
)

func callrecvmmsg(s int, msgvec uintptr, vlen uint32, flags int, timeout uintptr) (r1 uintptr, e1 Errno) {
	r1 = uintptr(C.recvmmsg(C.int(s), C.uintptr_t(msgvec), C.uint(vlen), C.int(flags), C.uintptr_t(timeout)))
	e1 = syscall.GetErrno()
	return
}

func callsendmmsg(s int, msg uintptr, vlen uint32, flags int) (r1 uintptr, e1 Errno) {
	r1 = uintptr(C.sendmmsg(C.int(s), C.uintptr_t(msg), C.uint(vlen), C.int(flags)))
	e1 = syscall.GetErrno()
	return
}
