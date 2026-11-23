package kcp

import (
	"net"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	libnetip "github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
)

type testingConnManager struct {
	connManager
}

func (manager *testingConnManager) getConn(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr) (*UDPSession, bool) {
	var loaded bool
	sess, _ := manager.Compute(laddr, remoteAddr, func(v *UDPSession, ok bool) (*UDPSession, xsync.ComputeOp) {
		if ok {
			loaded = ok
			return nil, xsync.CancelOp
		}

		return new(UDPSession), xsync.UpdateOp
	})
	return sess, loaded
}

// excepted has allocated memory
func BenchmarkConnManagerAllocs(b *testing.B) {
	manager := new(testingConnManager)
	manager.connManager = newConnManager()

	laddr := libnetip.AddrInterface{}
	remoteAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 54321}

	_, ok := manager.getConn(laddr, remoteAddr)
	if ok {
		b.Fatalf("unexcepted found")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, ok := manager.getConn(laddr, remoteAddr)
		if !ok {
			b.Fatal("unexcepted miss")
		}
	}
}

type testingConnManagerImpl struct {
	connManagerImpl
}

func (manager *testingConnManagerImpl) getConn(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr) (*UDPSession, bool) {
	var loaded bool
	sess, _ := manager.Compute(laddr, remoteAddr, func(v *UDPSession, ok bool) (*UDPSession, xsync.ComputeOp) {
		if ok {
			loaded = ok
			return nil, xsync.CancelOp
		}

		return new(UDPSession), xsync.UpdateOp
	})
	return sess, loaded
}

// excepted no allocated memory
func BenchmarkConnManagerImplAllocs(b *testing.B) {
	manager := new(testingConnManagerImpl)
	manager.connManager = newConnManager()

	laddr := libnetip.AddrInterface{}
	remoteAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 54321}

	_, ok := manager.getConn(laddr, remoteAddr)
	if ok {
		b.Fatalf("unexcepted found")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, ok := manager.getConn(laddr, remoteAddr)
		if !ok {
			b.Fatal("unexcepted miss")
		}
	}
}

