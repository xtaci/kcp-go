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

func TestConnManagerLoad(t *testing.T) {
	remote := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 54321}
	local := libnetip.AddrInterface{}
	want := new(UDPSession)

	for _, tc := range []struct {
		name    string
		manager connManagerImpl
	}{
		{name: "default", manager: connManagerImpl{connManager: newConnManager()}},
		{name: "unspecified", manager: connManagerImpl{connManager: newConnManagerUnspecified()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := tc.manager.Load(local, remote); ok || got != nil {
				t.Fatalf("Load(missing) = (%p, %v), want (nil, false)", got, ok)
			}

			tc.manager.Compute(local, remote, func(_ *UDPSession, _ bool) (*UDPSession, xsync.ComputeOp) {
				return want, xsync.UpdateOp
			})

			if got, ok := tc.manager.Load(local, remote); !ok || got != want {
				t.Fatalf("Load(present) = (%p, %v), want (%p, true)", got, ok, want)
			}
		})
	}
}

func TestConnManagerDefaultConnKey(t *testing.T) {
	manager := &connManagerImplDefault{}

	udpAddr := &net.UDPAddr{
		IP:   net.IPv4(192, 0, 2, 1),
		Port: 12345,
	}

	udpKey := manager.connKey(udpAddr)

	if udpKey.custom {
		t.Fatal("UDP address should not use a custom key")
	}

	if udpKey.remote != udpAddr.AddrPort() {
		t.Fatalf("unexpected UDP key: %+v", udpKey)
	}

	customKey := manager.connKey(netAddrTestStringAddr("peer:12345"))

	if !customKey.custom {
		t.Fatal("custom address should use a custom key")
	}

	if customKey.raddr != "peer:12345" {
		t.Fatalf("unexpected custom address key: %+v", customKey)
	}

	sameCustomKey := manager.connKey(netAddrTestStringAddr("peer:12345"))

	if customKey != sameCustomKey {
		t.Fatal("same custom addresses produced different keys")
	}

	differentCustomKey := manager.connKey(netAddrTestStringAddr("peer:12346"))

	if customKey == differentCustomKey {
		t.Fatal("different custom addresses produced the same key")
	}

	emptyCustomKey := manager.connKey(netAddrTestStringAddr(""))
	zeroUDPKey := manager.connKey(&net.UDPAddr{})

	if emptyCustomKey == zeroUDPKey {
		t.Fatal("custom and UDP keys should not collide")
	}
}

func TestConnManagerUnspecifiedRejectsCustomAddr(t *testing.T) {
	manager := &connManagerImplUnspecified{}

	_, ok := manager.connKey(
		libnetip.AddrInterface{},
		netAddrTestStringAddr("peer:12345"),
	)
	if ok {
		t.Fatal("custom address should not be accepted")
	}

	udpAddr := &net.UDPAddr{
		IP:   net.IPv4(192, 0, 2, 1),
		Port: 12345,
	}

	key, ok := manager.connKey(libnetip.AddrInterface{}, udpAddr)
	if !ok {
		t.Fatal("UDP address should be accepted")
	}

	if key.remote != udpAddr.AddrPort() {
		t.Fatalf("unexpected remote key: %+v", key)
	}
}
