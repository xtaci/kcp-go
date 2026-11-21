package syscall

import "golang.org/x/sys/unix"

type MsghdrX struct {
	Hdr unix.Msghdr
	Len uint64 /* user64_size_t msg_datalen; */
}

func (msghdrx *MsghdrX) SetLen(length int) {
	msghdrx.Len = uint64(length)
}
