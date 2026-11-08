// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

//go:build darwin

package kcp

import (
	"encoding/binary"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const darwinSockaddrSize = 128

// darwinBatchConn stores a UDP socket's RawConn and address family for Darwin batch syscalls.
type darwinBatchConn struct {
	raw    syscall.RawConn
	family int
}

// udpConn identifies UDP connections that expose their underlying socket through SyscallConn.
type udpConn interface {
	SyscallConn() (syscall.RawConn, error)
	ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error)
}

// darwinIovec matches Darwin's iovec layout and describes a data buffer.
type darwinIovec struct {
	base *byte
	len  uint64
}

// darwinMsghdrX matches Darwin's private msghdr_x ABI.
// Field order and widths must match the system header or the syscall will misinterpret the message array.
type darwinMsghdrX struct {
	name       *byte        // Pointer to the socket address.
	nameLen    uint32       // Size of the socket address, in bytes.
	iov        *darwinIovec // Pointer to the message's scatter/gather buffers.
	iovLen     int32        // Number of entries in iov.
	control    *byte        // Pointer to ancillary control data.
	controlLen uint32       // Size of the ancillary control data, in bytes.
	flags      int32        // Message flags set by recvmsg_x.
	dataLen    uint64       // Payload length, or buffer capacity for receive.
}

// newBatchConn creates a Darwin batch backend for PacketConns that expose an underlying UDP socket.
// The caller falls back to generic I/O for non-UDP connections or when RawConn is unavailable.
func newBatchConn(conn net.PacketConn) batchConn {
	udp, ok := conn.(udpConn)
	if !ok {
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp", conn.LocalAddr().String())
	if err != nil {
		return nil
	}
	// Determine the address family from the local address; unspecified and non-IPv4 addresses use IPv6.
	family := unix.AF_INET6
	if addr.IP.To4() != nil {
		family = unix.AF_INET
	}

	raw, err := udp.SyscallConn()
	if err != nil {
		return nil
	}
	return &darwinBatchConn{raw: raw, family: family}
}

// initPlatform initializes the session's Darwin-specific batch I/O backend.
func (sess *UDPSession) initPlatform() {
	sess.platform.batchConn = newBatchConn(sess.conn)
}

// WriteBatch converts messages to a Darwin msghdr_x array and sends them with sendmsg_x.
func (c *darwinBatchConn) WriteBatch(ms []ipv4.Message, flags int) (int, error) {
	if len(ms) == 0 {
		return 0, nil
	}

	// The syscall takes a contiguous header array. Names and each message's iovecs are allocated
	// separately and referenced by the headers; keep them alive until the syscall returns in RawConn.Write.
	headers := make([]darwinMsghdrX, len(ms))
	names := make([][darwinSockaddrSize]byte, len(ms))
	iovecs := make([][]darwinIovec, len(ms))
	for i := range ms {
		if len(ms[i].Buffers) == 0 {
			return 0, fmt.Errorf("darwin: batch message %d has no buffers", i)
		}
		if ms[i].Addr != nil {
			// Unconnected sockets need a destination address for each message; connected sockets may omit it.
			nameLen, err := c.encodeAddr(names[i][:], ms[i].Addr)
			if err != nil {
				return 0, err
			}
			headers[i].name = &names[i][0]
			headers[i].nameLen = nameLen
		}
		iovecs[i] = make([]darwinIovec, len(ms[i].Buffers))
		for j, buf := range ms[i].Buffers {
			// Represent empty slices with a nil base and zero length to avoid indexing buf[0].
			if len(buf) != 0 {
				iovecs[i][j].base = &buf[0]
			}
			iovecs[i][j].len = uint64(len(buf))
			headers[i].dataLen += uint64(len(buf))
		}
		headers[i].iov = &iovecs[i][0]
		headers[i].iovLen = int32(len(iovecs[i]))
	}

	return c.call(unix.SYS_SENDMSG_X, headers, flags)
}

// ReadBatch reads messages with recvmsg_x and copies the returned addresses and lengths into ipv4.Message.
func (c *darwinBatchConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	if len(ms) == 0 {
		return 0, nil
	}

	headers := make([]darwinMsghdrX, len(ms))
	names := make([][darwinSockaddrSize]byte, len(ms))
	iovecs := make([][]darwinIovec, len(ms))
	for i := range ms {
		if len(ms[i].Buffers) == 0 {
			return 0, fmt.Errorf("darwin: batch message %d has no buffers", i)
		}
		iovecs[i] = make([]darwinIovec, len(ms[i].Buffers))
		for j, buf := range ms[i].Buffers {
			if len(buf) != 0 {
				iovecs[i][j].base = &buf[0]
			}
			iovecs[i][j].len = uint64(len(buf))
		}
		headers[i].name = &names[i][0]
		// recvmsg_x writes the source sockaddr into this buffer and updates nameLen.
		headers[i].nameLen = darwinSockaddrSize
		headers[i].iov = &iovecs[i][0]
		headers[i].iovLen = int32(len(iovecs[i]))
	}

	count, err := c.call(unix.SYS_RECVMSG_X, headers, flags)
	if err != nil {
		return count, err
	}
	// The syscall returns the number of successful messages; only their headers and addresses are valid.
	for i := range count {
		ms[i].N = int(headers[i].dataLen)
		ms[i].Flags = int(headers[i].flags)
		addr, err := decodeDarwinAddr(names[i][:], headers[i].nameLen)
		if err != nil {
			return i, err
		}
		ms[i].Addr = addr
	}
	return count, nil
}

// call performs a Darwin socket syscall through a RawConn callback managed by the Go network runtime.
// RawConn.Read/Write retry when the file descriptor becomes ready; return false if it is not ready yet.
func (c *darwinBatchConn) call(trap uintptr, headers []darwinMsghdrX, flags int) (int, error) {
	var count int
	var callErr error
	call := func(fd uintptr) bool {
		r1, _, errno := unix.Syscall6(
			trap,
			fd,
			uintptr(unsafe.Pointer(&headers[0])),
			uintptr(len(headers)),
			uintptr(flags),
			0,
			0,
		)
		// The syscall accesses Go memory through unsafe pointers. Keep headers alive explicitly;
		// the referenced address and iovec slices remain reachable through their elements.
		runtime.KeepAlive(headers)
		// Return false for retryable errors so the runtime waits for the file descriptor and retries.
		if errno == unix.EINTR || errno == unix.EAGAIN || errno == unix.EWOULDBLOCK {
			return false
		}
		if errno != 0 {
			callErr = errno
			return true
		}
		count = int(r1)
		return true
	}

	var err error
	// RawConn.Read/Write mark the operation as a read or write for the runtime poller.
	if trap == unix.SYS_RECVMSG_X {
		err = c.raw.Read(call)
	} else {
		err = c.raw.Write(call)
	}
	if err != nil {
		return 0, err
	}
	if callErr != nil {
		return 0, callErr
	}
	return count, nil
}

// encodeAddr encodes a Go UDPAddr using Darwin's sockaddr_in or sockaddr_in6 layout.
// The first two sockaddr bytes contain the structure length and address family; the port uses network byte order.
func (c *darwinBatchConn) encodeAddr(dst []byte, addr net.Addr) (uint32, error) {
	udp, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("darwin: unsupported batch destination address %T", addr)
	}
	if udp.Port < 0 || udp.Port > 65535 {
		return 0, fmt.Errorf("darwin: invalid UDP destination port %d", udp.Port)
	}

	switch c.family {
	case unix.AF_INET:
		ip := udp.IP.To4()
		if ip == nil {
			return 0, fmt.Errorf("darwin: IPv6 destination %q on an IPv4 socket", udp)
		}
		dst[0] = 16
		dst[1] = unix.AF_INET
		// Bytes [2:4] hold the network-byte-order port.
		binary.BigEndian.PutUint16(dst[2:4], uint16(udp.Port))
		// Bytes [4:8] hold the IPv4 address.
		copy(dst[4:8], ip)
		return 16, nil
	case unix.AF_INET6:
		ip := udp.IP.To16()
		if ip == nil {
			return 0, fmt.Errorf("darwin: invalid UDP destination %q", udp)
		}
		dst[0] = 28
		dst[1] = unix.AF_INET6
		// Bytes [2:4] hold the network-byte-order port.
		binary.BigEndian.PutUint16(dst[2:4], uint16(udp.Port))
		if v4 := udp.IP.To4(); v4 != nil {
			// Encode an IPv4 destination on an IPv6 socket as an IPv4-mapped IPv6 address.
			// Bytes [8:18] are zero, [18:20] are 0xffff, and [20:24] hold the IPv4 address.
			for j := 0; j < 10; j++ {
				dst[8+j] = 0
			}
			dst[18], dst[19] = 0xff, 0xff
			copy(dst[20:24], v4)
		} else {
			// Bytes [8:24] hold the IPv6 address.
			copy(dst[8:24], ip)
		}
		if udp.Zone != "" {
			// sockaddr_in6 uses a local interface index for scope ID, while Go's Zone is an interface name.
			iface, err := net.InterfaceByName(udp.Zone)
			if err != nil {
				return 0, fmt.Errorf("darwin: resolve IPv6 zone %q: %w", udp.Zone, err)
			}
			// Bytes [24:28] hold the little-endian IPv6 scope ID.
			binary.LittleEndian.PutUint32(dst[24:28], uint32(iface.Index))
		}
		return 28, nil
	default:
		return 0, fmt.Errorf("darwin: unsupported socket address family %d", c.family)
	}
}

// decodeDarwinAddr decodes a Darwin sockaddr returned by recvmsg_x into a Go UDPAddr.
func decodeDarwinAddr(src []byte, nameLen uint32) (*net.UDPAddr, error) {
	if nameLen < 4 {
		return nil, fmt.Errorf("darwin: invalid socket address length %d", nameLen)
	}
	// Bytes [2:4] contain the network-byte-order port.
	port := int(binary.BigEndian.Uint16(src[2:4]))
	switch src[1] {
	case unix.AF_INET:
		if nameLen < 16 {
			return nil, fmt.Errorf("darwin: invalid IPv4 socket address length %d", nameLen)
		}
		// Bytes [4:8] contain the IPv4 address.
		return &net.UDPAddr{IP: net.IPv4(src[4], src[5], src[6], src[7]), Port: port}, nil
	case unix.AF_INET6:
		if nameLen < 28 {
			return nil, fmt.Errorf("darwin: invalid IPv6 socket address length %d", nameLen)
		}
		// Bytes [8:24] contain the IPv6 address.
		addr := &net.UDPAddr{IP: append(net.IP(nil), src[8:24]...), Port: port}
		// Bytes [24:28] contain the little-endian IPv6 scope ID.
		if zone := binary.LittleEndian.Uint32(src[24:28]); zone != 0 {
			// Convert the scope ID back to the interface name used by net.UDPAddr, if the interface still exists.
			if iface, err := net.InterfaceByIndex(int(zone)); err == nil {
				addr.Zone = iface.Name
			}
		}
		return addr, nil
	default:
		return nil, fmt.Errorf("darwin: unsupported socket address family %d", src[1])
	}
}
