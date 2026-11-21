package syscall

import "golang.org/x/sys/unix"

type Mmsghdr struct {
	Hdr unix.Msghdr
	Len uint32
}

func (mmsghdr *Mmsghdr) SetLen(length int) {
	mmsghdr.Len = uint32(length)
}
