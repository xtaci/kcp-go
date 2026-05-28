package kcp

import (
	"net"
	"sync"
	"testing"
	"time"
)

type fakePacketConn struct {
	addr net.Addr
}

func (f *fakePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {}
}

func (f *fakePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return len(p), nil
}

func (f *fakePacketConn) Close() error                       { return nil }
func (f *fakePacketConn) LocalAddr() net.Addr                { return f.addr }
func (f *fakePacketConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakePacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakePacketConn) SetWriteDeadline(t time.Time) error { return nil }

func buildKCPPushPacket(conv uint32) []byte {
	buf := make([]byte, IKCP_OVERHEAD)
	buf[0] = byte(conv)
	buf[1] = byte(conv >> 8)
	buf[2] = byte(conv >> 16)
	buf[3] = byte(conv >> 24)
	buf[4] = IKCP_CMD_PUSH
	buf[5] = 0
	buf[6] = 32
	buf[7] = 0
	return buf
}

// TestListenerDedupSameAddr is a regression test for xtaci/kcp-go#340:
// when multiple packets from the same remote address race the Listener's
// session-tracking map, AcceptKCP can hand out duplicate UDPSession objects
// for the same RemoteAddr. We expect exactly one session per (addr, conv).
func TestListenerDedupSameAddr(t *testing.T) {
	l := new(Listener)
	l.conn = &fakePacketConn{addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}}
	l.ownConn = false
	l.sessions = make(map[string]*UDPSession)
	l.chAccepts = make(chan *UDPSession, acceptBacklog)
	l.chSocketReadError = make(chan struct{})
	l.die = make(chan struct{})

	remote := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 50000}
	const conv = uint32(0xCAFEBABE)
	pkt := buildKCPPushPacket(conv)

	const N = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			data := make([]byte, len(pkt))
			copy(data, pkt)
			l.packetInput(data, remote)
		}()
	}
	close(start)
	wg.Wait()

	accepted := 0
	drain := true
	for drain {
		select {
		case s := <-l.chAccepts:
			accepted++
			if s.RemoteAddr().String() != remote.String() {
				t.Errorf("accepted session has wrong RemoteAddr=%s, want %s",
					s.RemoteAddr(), remote)
			}
		default:
			drain = false
		}
	}
	if accepted != 1 {
		t.Fatalf("Listener handed out %d sessions for a single (addr, conv); want 1 (issue #340)", accepted)
	}

	l.sessionLock.RLock()
	mapSize := len(l.sessions)
	_, ok := l.sessions[remote.String()]
	l.sessionLock.RUnlock()
	if mapSize != 1 || !ok {
		t.Fatalf("Listener.sessions has %d entries (expected 1) and contains %q=%v",
			mapSize, remote.String(), ok)
	}
}
