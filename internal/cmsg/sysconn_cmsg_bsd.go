//go:build freebsd || openbsd || netbsd || dragonfly

package cmsg

import (
	"errors"
	"net"
	"syscall"
	"unsafe"

	"github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
	"golang.org/x/sys/unix"
)

func buildSocketControlMessage(hdr []unix.Cmsghdr, data [][]byte) *[]byte {
	if len(hdr) != len(data) {
		panic("invalid arguments: hdr and data must be the same length")
	}

	if len(hdr) == 0 {
		return nil
	}

	n := 0
	for i := range hdr {
		hdr[i].SetLen(unix.CmsgLen(len(data[i])))
		n += unix.CmsgSpace(len(data[i]))
	}

	offset := 0
	cmsg := make([]byte, n)
	for i := range hdr {
		*(*unix.Cmsghdr)(unsafe.Pointer(&cmsg[offset])) = hdr[i]
		copy(cmsg[offset+unix.CmsgLen(0):], data[i])
		offset += unix.CmsgSpace(len(data[i]))
	}

	return &cmsg
}

///////////////

const SupportsControlMessage = true

func SetupControlMessage(c syscall.RawConn) error {
	var ret4, ret6 error
	err := c.Control(func(fd uintptr) {
		ret4 = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVDSTADDR, 1)
		ret6 = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVPKTINFO, 1)
	})
	if err != nil {
		return err
	}
	if ret4 != nil && ret6 != nil {
		return errors.Join(ret4, ret6)
	}
	return nil
}

func ParseControlMessage(data []byte) (netip.AddrInterface, error) {
	for len(data) > 0 {
		hdr, body, remainder, err := unix.ParseOneSocketControlMessage(data)
		if err != nil {
			return netip.AddrInterface{}, err
		}
		data = remainder

		switch hdr.Level {
		case unix.IPPROTO_IP:
			switch hdr.Type {
			case unix.IP_RECVDSTADDR:
				if len(body) < net.IPv4len {
					return netip.AddrInterface{}, unix.EINVAL
				}
				ip := *(*[net.IPv4len]byte)(unsafe.Pointer(&body[0]))

				return netip.AddrInterfaceFrom(
					netip.AddrFrom4(ip),
					0,
				), nil
			}

		case unix.IPPROTO_IPV6:
			switch hdr.Type {
			case unix.IPV6_PKTINFO:
				if len(body) < unix.SizeofInet6Pktinfo {
					return netip.AddrInterface{}, unix.EINVAL
				}

				info := (*unix.Inet6Pktinfo)(unsafe.Pointer(&body[0]))
				return netip.AddrInterfaceFrom(
					netip.AddrFrom16(info.Addr),
					info.Ifindex,
				), nil
			}
		}
	}

	return netip.AddrInterface{}, errors.New("no valid control message found")
}

func buildControlMessage(laddr netip.AddrInterface) *[]byte {
	addr := laddr.Addr()

	if addr.Is4() {
		hdr := make([]unix.Cmsghdr, 0, 1)
		data := make([][]byte, 0, 1)

		{
			src := addr.As4()

			hdr = append(hdr, unix.Cmsghdr{Level: unix.IPPROTO_IP, Type: unix.IP_RECVDSTADDR})
			data = append(data, unsafe.Slice((*byte)(unsafe.Pointer(&src)), net.IPv4len))
		}

		return buildSocketControlMessage(hdr, data)
	}

	hdr := make([]unix.Cmsghdr, 0, 1)
	data := make([][]byte, 0, 1)

	{
		info := unix.Inet6Pktinfo{
			Addr:    addr.As16(),
			Ifindex: laddr.IfIndex(),
		}

		hdr = append(hdr, unix.Cmsghdr{Level: unix.IPPROTO_IPV6, Type: unix.IPV6_PKTINFO})
		data = append(data, unsafe.Slice((*byte)(unsafe.Pointer(&info)), unix.SizeofInet6Pktinfo))
	}

	return buildSocketControlMessage(hdr, data)
}
