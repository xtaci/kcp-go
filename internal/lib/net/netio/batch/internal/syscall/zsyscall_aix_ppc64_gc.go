//go:build aix && ppc64 && gc

package syscall

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

type syscallFunc uintptr

//go:linkname rawSyscall6 syscall.rawSyscall6
func rawSyscall6(trap, nargs, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err unix.Errno)

//go:linkname syscall6 syscall.syscall6
func syscall6(trap, nargs, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err unix.Errno)

func callrecvmmsg(s int, msgvec uintptr, vlen uint32, flags int, timeout uintptr) (r0 uintptr, e1 unix.Errno) {
	r0, _, e1 = syscall6(uintptr(unsafe.Pointer(&libc_recvmmsg)), 5, uintptr(s), msgvec, uintptr(vlen), uintptr(flags), timeout, 0)
	return
}

//go:cgo_import_dynamic libc_recvmmsg recvmmsg "libc.a/shr_64.o"
var libc_recvmmsg syscallFunc

func callsendmmsg(s int, msg uintptr, vlen uint32, flags int) (r0 uintptr, e1 unix.Errno) {
	r0, _, e1 = syscall6(uintptr(unsafe.Pointer(&libc_sendmmsg)), 4, uintptr(s), msg, uintptr(vlen), uintptr(flags), 0, 0)
	return
}

//go:cgo_import_dynamic libc_sendmmsg sendmmsg "libc.a/shr_64.o"
var libc_sendmmsg syscallFunc
