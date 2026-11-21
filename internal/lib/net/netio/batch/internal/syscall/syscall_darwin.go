package syscall

// RecvMsgs receives up to len(hdrs) datagrams in one recvmsg_x call.
//
// The kernel fills hdrs[i].Msg.Name, hdrs[i].Msg.Namelen,
// hdrs[i].Msg.Control, hdrs[i].Msg.Controllen, hdrs[i].Msg.Flags, and
// hdrs[i].Datalen for the first n entries. Entries at index >= n are
// left untouched.
//
// flags accepts the standard MSG_* flags.
//
// # OS compatibility
//
// recvmsg_x and sendmsg_x are private, unsupported XNU syscalls (480 and
// 481). Each entry below names the release that introduced a change.
//
//   - OS X 10.11 / iOS 9.0 (xnu-3248): recvmsg_x and sendmsg_x are introduced.
//     Data delivery works; cmsg does not.
//
//   - macOS 11.3 / iOS 14.6 (xnu-7195.101.1): externalize_recv_msghdr_array starts
//     calling copyout_control and writes back msg_namelen and
//     msg_controllen. Before this release cmsg buffers are inert: the
//     kernel never copies msg_control, and msg_controllen keeps whatever
//     value the caller passed. Zero-initialize it before every call on
//     pre-11.3 systems; a stale value is the only observable state.
//
//   - macOS 15.6 / iOS 18.6 (xnu-11417.140.69): fixes a socket lock leak in
//     soreceive_m_list. Before this release, two or more concurrent
//     recvmsg_x calls on one socket can deadlock the kernel's loopback
//     input thread, black-holing all 127.0.0.1 / ::1 traffic until
//     reboot. The single-readloop usage in this package never triggers
//     it, but do not call RecvMsgs from multiple goroutines on one
//     socket below 15.6.
//
//   - Latest (xnu-12377.1.9): truncation still silent. When a datagram exceeds
//     its iovec, XNU clamps msg_datalen to the buffer size and leaves
//     msg_flags zeroed. MSG_TRUNC is not raised (unlike plain recvmsg).
//     A Message whose N equals len(Payload) may have been truncated;
//     size Payload above the largest expected datagram, because nothing
//     distinguishes an exact fit from a loss.
//
// The caller is responsible for pinning any Go memory referenced by
// hdrs[i].Msg.Iov[i].Base for the duration of the call.
func RecvMsgs(fd uintptr, hdrs []MsghdrX, flags int) (n int, err error) {
	if len(hdrs) == 0 {
		return 0, nil
	}
	return recvmsg_x(int(fd), &hdrs[0], uint32(len(hdrs)), flags)
}

// SendMsgs transmits up to len(hdrs) datagrams in one sendmsg_x call.
//
// msg_name, msg_namelen, msg_control, and msg_controllen must be zero
// on input, and so must hdrs[i].Datalen; socket_private.h requires it.
// On a connected socket the kernel batches the messages into one
// protocol-stack entry. On an unconnected socket, or when any message
// carries address or control data, XNU falls back to per-packet
// sendmsg internally; the call still succeeds and still transmits every
// datagram, but without the batch fast path.
//
// The kernel may accept fewer datagrams than offered. n reports how
// many it took; the caller retries the unaccepted tail.
//
// Available since OS X 10.11 (xnu-3248).
//
// The caller is responsible for pinning any Go memory referenced by
// hdrs[i].Msg.Iov[i].Base for the duration of the call.
func SendMsgs(fd uintptr, hdrs []MsghdrX, flags int) (n int, err error) {
	if len(hdrs) == 0 {
		return 0, nil
	}
	return sendmsg_x(int(fd), &hdrs[0], uint32(len(hdrs)), flags)
}
