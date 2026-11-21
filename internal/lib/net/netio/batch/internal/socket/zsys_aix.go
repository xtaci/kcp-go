package socket

import "github.com/xtaci/kcp-go/v5/internal/lib/net/netio/batch/internal/syscall"

type mmsghdr = syscall.Mmsghdr

func recvmsgs(fd uintptr, hs []mmsghdr, flags int) (int, error) {
	return syscall.RecvMsgs(fd, hs, flags, nil)
}

func sendmsgs(fd uintptr, hs []mmsghdr, flags int) (int, error) {
	return syscall.SendMsgs(fd, hs, flags)
}
