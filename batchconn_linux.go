// +build linux

package kcp

import (
	"net"
	"os"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func toBatchConn(c net.PacketConn) batchConn {
	if xconn, ok := c.(batchConn); ok {
		return xconn
	}
	var xconn batchConn
	if _, ok := c.(*net.UDPConn); ok {
		addr, err := net.ResolveUDPAddr("udp", c.LocalAddr().String())
		if err == nil {
			if addr.IP.To4() != nil {
				xconn = ipv4.NewPacketConn(c)
			} else {
				xconn = ipv6.NewPacketConn(c)
			}
		}
	}
	return xconn
}

func isPacketConn(xconn batchConn) bool {
	if _, ok := xconn.(*ipv4.PacketConn); ok {
		return true
	}
	if _, ok := xconn.(*ipv6.PacketConn); ok {
		return true
	}
	return false
}

func readBatchUnavailable(xconn batchConn, err error) bool {
	if isPacketConn(xconn) {
		// compatibility issue:
		// for linux kernel<=2.6.32, support for sendmmsg is not available
		// an error of type os.SyscallError will be returned
		if operr, ok := err.(*net.OpError); ok {
			if se, ok := operr.Err.(*os.SyscallError); ok {
				if se.Syscall == "recvmmsg" {
					return true
				}
			}
		}
		return false
	}
	ret := false
	if detector, ok := xconn.(batchErrDetector); ok {
		ret = detector.ReadBatchUnavailable(err)
	}
	return ret
}

func writeBatchUnavailable(xconn batchConn, err error) bool {
	if isPacketConn(xconn) {
		// compatibility issue:
		// for linux kernel<=2.6.32, support for sendmmsg is not available
		// an error of type os.SyscallError will be returned
		if operr, ok := err.(*net.OpError); ok {
			if se, ok := operr.Err.(*os.SyscallError); ok {
				if se.Syscall == "sendmmsg" {
					return true
				}
			}
		}
		return false
	}
	ret := false
	if detector, ok := xconn.(batchErrDetector); ok {
		ret = detector.WriteBatchUnavailable(err)
	}
	return ret
}
