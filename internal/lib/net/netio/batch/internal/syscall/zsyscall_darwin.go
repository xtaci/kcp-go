package syscall

import (
	"unsafe"
)

func sendmsg_x(s int, msg *MsghdrX, cnt uint32, flags int) (n int, err error) {
	r0, _, e1 := syscall_syscall6(libc_sendmsg_x_trampoline_addr, uintptr(s), uintptr(unsafe.Pointer(msg)), uintptr(cnt), uintptr(flags), 0, 0)
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

var libc_sendmsg_x_trampoline_addr uintptr

//go:cgo_import_dynamic libc_sendmsg_x sendmsg_x "/usr/lib/libSystem.B.dylib"

func recvmsg_x(s int, msg *MsghdrX, cnt uint32, flags int) (n int, err error) {
	r0, _, e1 := syscall_syscall6(libc_recvmsg_x_trampoline_addr, uintptr(s), uintptr(unsafe.Pointer(msg)), uintptr(cnt), uintptr(flags), 0, 0)
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}

	return
}

var libc_recvmsg_x_trampoline_addr uintptr

//go:cgo_import_dynamic libc_recvmsg_x recvmsg_x "/usr/lib/libSystem.B.dylib"
