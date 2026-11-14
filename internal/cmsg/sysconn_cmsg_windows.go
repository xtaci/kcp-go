package cmsg

import (
	"errors"
	"syscall"
	"unsafe"

	"github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
	"golang.org/x/sys/windows"
)

const (
	sizeofPtr        = int(unsafe.Sizeof(uintptr(0)))
	sizeofWSACMSGHDR = int(unsafe.Sizeof(windows.WSACMSGHDR{}))
	sizeofInPktinfo  = int(unsafe.Sizeof(windows.IN_PKTINFO{}))
	sizeofIn6Pktinfo = int(unsafe.Sizeof(windows.IN6_PKTINFO{}))
)

func cmsgAlignOf(salen int) int {
	salign := sizeofPtr
	return (salen + salign - 1) &^ (salign - 1)
}

func socketControlMessageHeaderAndData(b []byte) (*windows.WSACMSGHDR, []byte, error) {
	h := (*windows.WSACMSGHDR)(unsafe.Pointer(&b[0]))
	if h.Len < uintptr(sizeofWSACMSGHDR) || uint64(h.Len) > uint64(len(b)) {
		return nil, nil, windows.WSAEINVAL
	}

	return h, b[cmsgAlignOf(sizeofWSACMSGHDR):h.Len], nil
}

func parseOneSocketControlMessage(b []byte) (hdr windows.WSACMSGHDR, dbuf []byte, remainder []byte, err error) {
	h, dbuf, err := socketControlMessageHeaderAndData(b)
	if err != nil {
		return windows.WSACMSGHDR{}, nil, nil, err
	}
	if i := cmsgAlignOf(int(h.Len)); i < len(b) {
		remainder = b[i:]
	}
	return *h, dbuf, remainder, nil
}

func buildSocketControlMessage(hdr []windows.WSACMSGHDR, data [][]byte) *[]byte {
	if len(hdr) != len(data) {
		panic("invalid arguments: hdr and data must be the same length")
	}

	if len(hdr) == 0 {
		return nil
	}

	n := 0
	for i := range hdr {
		hdr[i].Len = uintptr(sizeofWSACMSGHDR + len(data[i]))
		n += cmsgAlignOf(int(hdr[i].Len))
	}

	offset := 0
	cmsg := make([]byte, n)
	for i := range hdr {
		*(*windows.WSACMSGHDR)(unsafe.Pointer(&cmsg[offset])) = hdr[i]
		copy(cmsg[offset+sizeofWSACMSGHDR:], data[i])
		offset += cmsgAlignOf(int(hdr[i].Len))
	}
	return &cmsg
}

///////////////

const SupportsControlMessage = true

func SetupControlMessage(c syscall.RawConn) error {
	var ret4, ret6 error
	err := c.Control(func(fd uintptr) {
		ret4 = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_PKTINFO, 1)
		ret6 = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, windows.IPV6_PKTINFO, 1)
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
		hdr, body, remainder, err := parseOneSocketControlMessage(data)
		if err != nil {
			return netip.AddrInterface{}, err
		}
		data = remainder

		switch hdr.Level {
		case windows.IPPROTO_IP:
			switch hdr.Type {
			case windows.IP_PKTINFO:
				if len(body) < sizeofInPktinfo {
					return netip.AddrInterface{}, windows.WSAEINVAL
				}

				info := (*windows.IN_PKTINFO)(unsafe.Pointer(&body[0]))
				return netip.AddrInterfaceFrom(
					netip.AddrFrom4(info.Addr),
					info.Ifindex,
				), nil
			}
		case windows.IPPROTO_IPV6:
			switch hdr.Type {
			case windows.IPV6_PKTINFO:
				if len(body) < sizeofIn6Pktinfo {
					return netip.AddrInterface{}, windows.WSAEINVAL
				}

				info := (*windows.IN6_PKTINFO)(unsafe.Pointer(&body[0]))
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
		hdr := make([]windows.WSACMSGHDR, 0, 1)
		data := make([][]byte, 0, 1)

		{
			info := windows.IN_PKTINFO{
				Addr:    addr.As4(),
				Ifindex: laddr.IfIndex(),
			}

			hdr = append(hdr, windows.WSACMSGHDR{Level: windows.IPPROTO_IP, Type: windows.IP_PKTINFO})
			data = append(data, unsafe.Slice((*byte)(unsafe.Pointer(&info)), sizeofInPktinfo))
		}

		return buildSocketControlMessage(hdr, data)
	}

	hdr := make([]windows.WSACMSGHDR, 0, 1)
	data := make([][]byte, 0, 1)

	{
		info := windows.IN6_PKTINFO{
			Addr:    addr.As16(),
			Ifindex: laddr.IfIndex(),
		}

		hdr = append(hdr, windows.WSACMSGHDR{Level: windows.IPPROTO_IPV6, Type: windows.IPV6_PKTINFO})
		data = append(data, unsafe.Slice((*byte)(unsafe.Pointer(&info)), sizeofIn6Pktinfo))
	}

	return buildSocketControlMessage(hdr, data)
}
