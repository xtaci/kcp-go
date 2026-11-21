package syscall

import "golang.org/x/sys/unix"

// RecvMsgs receives up to len(hdrs) datagrams in one recvmmsg call.
//
// The kernel fills hdrs[i].Hdr.Name, hdrs[i].Hdr.Namelen,
// hdrs[i].Hdr.Control, hdrs[i].Hdr.Controllen, hdrs[i].Hdr.Flags, and
// hdrs[i].Len for the first n entries. Entries at index >= n are left
// untouched.
//
// flags accepts the standard MSG_* flags. On truncation the kernel sets
// MSG_TRUNC in hdrs[i].Hdr.Flags and reports the truncated length in
// hdrs[i].Len; size each iovec above the largest expected datagram.
//
// RecvMsgs calls recvmmsg with a nil timeout: it blocks until at least
// one datagram arrives, or returns EAGAIN immediately if the socket is
// non-blocking. It does not park the goroutine on the netpoll; on a
// blocking socket it ties up the calling OS thread. Prefer a
// non-blocking socket, or wrap with syscall.RawConn.Read.
//
// The caller is responsible for keeping the memory referenced by
// hdrs[i].Hdr.Iov[j].Base reachable and unmoved for the duration of the
// call.
func RecvMsgs(fd uintptr, hdrs []Mmsghdr, flags uint32, timeout *unix.Timespec) (n int, err error) {
	if len(hdrs) == 0 {
		return 0, nil
	}
	return recvmmsg(int(fd), &hdrs[0], uint32(len(hdrs)), flags, timeout)
}

// SendMsgs transmits up to len(hdrs) datagrams in one sendmmsg call.
//
// Each hdrs[i].Hdr.Name must describe the destination of message i and
// hdrs[i].Hdr.Namelen its length. hdrs[i].Hdr.Control,
// hdrs[i].Hdr.Controllen, hdrs[i].Hdr.Flags, and hdrs[i].Len must be
// zero on input; the kernel overwrites hdrs[i].Len with the byte count
// it accepted for message i.
//
// The kernel may accept fewer datagrams than offered. n reports how many
// it took; the caller retries the unaccepted tail.
//
// Like RecvMsgs, SendMsgs does not park on the netpoll. On a blocking
// socket the send blocks the calling OS thread; on a non-blocking
// socket it returns EAGAIN when the send buffer is full.
//
// The caller is responsible for keeping the memory referenced by
// hdrs[i].Hdr.Iov[j].Base reachable and unmoved for the duration of the
// call.
func SendMsgs(fd uintptr, hdrs []Mmsghdr, flags uint32) (n int, err error) {
	if len(hdrs) == 0 {
		return 0, nil
	}
	return sendmmsg(int(fd), &hdrs[0], uint32(len(hdrs)), flags)
}
