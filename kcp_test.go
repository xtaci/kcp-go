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

package kcp

import (
	"bytes"
	crand "crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xtaci/lossyconn"
)

const (
	lossyTestRepeat  = 8
	lossyTestTimeout = 20 * time.Second
)

type lossyTestCase struct {
	name         string
	config       lossyconn.Config
	nc           int
	outage       time.Duration
	wantRandom   bool
	wantBurst    bool
	wantDup      bool
	wantCorrupt  bool
	wantReorder  bool
	wantLinkDown bool
}

// netAddrFactory supplies distinct UDP addresses to the lossy connection tests.
func netAddrFactory() net.Addr {
	ip := make(net.IP, net.IPv6len)
	ip[0] = 0x20
	ip[1] = 0x01
	ip[2] = 0x0d
	ip[3] = 0xb8
	_, err := io.ReadFull(crand.Reader, ip[4:])
	if err != nil {
		panic(fmt.Sprintf("failed to generate random IP: %v", err))
	}

	return &net.UDPAddr{
		IP:   ip,
		Port: rand.Intn(65535) + 1,
	}
}

func TestLossyConn(t *testing.T) {
	tests := []lossyTestCase{
		{
			name: "random loss",
			config: lossyconn.Config{
				Delay:          10 * time.Millisecond,
				RandomLossRate: 0.1,
				Seed:           1,
			},
			nc:         1,
			wantRandom: true,
		},
		{
			name: "burst loss and congestion control",
			config: lossyconn.Config{
				Delay:         5 * time.Millisecond,
				BurstLossRate: 0.1,
				BurstLength:   3,
				Seed:          7,
			},
			nc:        0,
			wantBurst: true,
		},
		{
			name: "duplicate corrupt and reorder",
			config: lossyconn.Config{
				Delay:           5 * time.Millisecond,
				DuplicateRate:   0.2,
				CorruptionRate:  0.1,
				ReorderRate:     0.3,
				MaxReorderDelay: 20 * time.Millisecond,
				Seed:            13,
			},
			nc:          1,
			wantDup:     true,
			wantCorrupt: true,
			wantReorder: true,
		},
		{
			name: "temporary link outage and recovery",
			config: lossyconn.Config{
				Delay: 5 * time.Millisecond,
				Seed:  19,
			},
			outage:       250 * time.Millisecond,
			wantLinkDown: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientConfig, serverConfig := tt.config, tt.config
			serverConfig.Seed++
			client, err := lossyconn.NewWithConfigAndAddrFactory(clientConfig, netAddrFactory)
			if err != nil {
				t.Fatal(err)
			}
			server, err := lossyconn.NewWithConfigAndAddrFactory(serverConfig, netAddrFactory)
			if err != nil {
				_ = client.Close()
				t.Fatal(err)
			}
			var block BlockCrypt
			if tt.wantCorrupt {
				block, err = NewAESGCMCrypt([]byte("kcp-lossy-test-key-32-bytes-long"))
				if err != nil {
					_ = client.Close()
					_ = server.Close()
					t.Fatal(err)
				}
			}
			testlink(t, client, server, block, 1, 10, 2, tt.nc, tt)
		})
	}
}

func testlink(t *testing.T, client, server *lossyconn.LossyConn, block BlockCrypt, nodelay, interval, resend, nc int, tt lossyTestCase) {
	t.Helper()
	sess, err := NewConn2(server.LocalAddr(), block, 0, 0, client)
	if err != nil {
		_ = client.Close()
		_ = server.Close()
		t.Fatal(err)
	}
	listener, err := ServeConn(block, 0, 0, server)
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		_ = server.Close()
		t.Fatal(err)
	}

	done := make(chan struct{})
	serverDone := make(chan struct{})
	serverErr := make(chan error, 1)
	serverSessions := make(chan *UDPSession, 1)
	go func() {
		defer close(serverDone)
		conn, err := listener.AcceptKCP()
		if err != nil {
			return
		}
		serverSessions <- conn
		if err := conn.SetDeadline(time.Now().Add(lossyTestTimeout)); err != nil {
			serverErr <- fmt.Errorf("set echo server deadline: %w", err)
			return
		}
		conn.SetNoDelay(nodelay, interval, resend, nc)
		buf := make([]byte, 65536)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				select {
				case <-done:
				default:
					serverErr <- fmt.Errorf("echo server read: %w", err)
				}
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				select {
				case <-done:
				default:
					serverErr <- fmt.Errorf("echo server write: %w", err)
				}
				return
			}
		}
	}()

	defer func() {
		close(done)
		var conn *UDPSession
		select {
		case conn = <-serverSessions:
		default:
		}
		if conn != nil {
			_ = conn.Close()
		}
		_ = listener.Close()
		_ = sess.Close()
		_ = client.Close()
		_ = server.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("echo server did not stop")
		}
		select {
		case err := <-serverErr:
			t.Error(err)
		default:
		}
	}()

	if err := sess.SetDeadline(time.Now().Add(lossyTestTimeout)); err != nil {
		t.Fatal(err)
	}
	sess.SetNoDelay(nodelay, interval, resend, nc)
	payload := make([]byte, 4096)
	response := make([]byte, len(payload))
	var totalRTT time.Duration
	for round := range lossyTestRepeat {
		for i := range payload {
			payload[i] = byte(i + round)
		}
		start := time.Now()
		n, err := sess.Write(payload)
		if err != nil {
			t.Fatalf("write round %d: %v", round, err)
		}
		if n != len(payload) {
			t.Fatalf("write round %d: wrote %d bytes, want %d", round, n, len(payload))
		}
		if _, err := io.ReadFull(sess, response); err != nil {
			t.Fatalf("read round %d: %v", round, err)
		}
		if !bytes.Equal(response, payload) {
			t.Fatalf("echo mismatch in round %d", round)
		}
		totalRTT += time.Since(start)
	}

	if tt.outage > 0 {
		client.SetLinkState(false)
		server.SetLinkState(false)
		payload = bytes.Repeat([]byte("link outage recovery"), 256)
		if _, err := sess.Write(payload); err != nil {
			t.Fatalf("write during link outage: %v", err)
		}
		time.Sleep(tt.outage)
		client.SetLinkState(true)
		server.SetLinkState(true)
		response = make([]byte, len(payload))
		if _, err := io.ReadFull(sess, response); err != nil {
			t.Fatalf("read after link recovery: %v", err)
		}
		if !bytes.Equal(response, payload) {
			t.Fatal("echo mismatch after link recovery")
		}
	}

	serverStats := server.Stats()
	clientStats := client.Stats()
	t.Logf("client stats: %+v", clientStats)
	t.Logf("server stats: %+v", serverStats)
	t.Logf("average round trip: %v", totalRTT/lossyTestRepeat)
	checkLossyStats(t, tt, clientStats, serverStats)
}

func checkLossyStats(t *testing.T, tt lossyTestCase, client, server lossyconn.Stats) {
	t.Helper()
	if tt.wantRandom && client.DropRandom+server.DropRandom == 0 {
		t.Error("random loss profile did not drop any packets")
	}
	if tt.wantBurst && client.DropBurst+server.DropBurst == 0 {
		t.Error("burst loss profile did not drop any packets")
	}
	if tt.wantDup && client.DuplicatedPackets+server.DuplicatedPackets == 0 {
		t.Error("duplicate profile did not duplicate any packets")
	}
	if tt.wantCorrupt && client.CorruptedPackets+server.CorruptedPackets == 0 {
		t.Error("corruption profile did not corrupt any packets")
	}
	if tt.wantReorder && client.Reordered+server.Reordered == 0 {
		t.Error("reorder profile did not reorder any packets")
	}
	if tt.wantLinkDown && client.DropLinkDown+server.DropLinkDown == 0 {
		t.Error("link outage did not drop any packets")
	}
}

func BenchmarkFlush(b *testing.B) {
	kcp := NewKCP(1, func(buf []byte, size int) {})
	kcp.snd_buf = NewRingBuffer[segment](1024)
	for range kcp.snd_buf.MaxLen() {
		kcp.snd_buf.Push(segment{xmit: 1, resendts: currentMs() + 10000})
	}

	b.ReportAllocs()
	var mu sync.Mutex
	for b.Loop() {
		mu.Lock()
		kcp.flush(IKCP_FLUSH_FULL)
		mu.Unlock()
	}
}

// TestSegmentHeap tests the segmentHeap data structure
func TestSegmentHeap(t *testing.T) {
	h := newSegmentHeap()
	segments := []segment{
		{sn: 1},
		{sn: 2},
		{sn: 3},
	}

	for _, seg := range segments {
		h.Push(seg)
		t.Logf("pushed segment with seq %d", seg.sn)
	}

	if h.Len() != len(segments) {
		t.Errorf("expected length %d, got %d", len(segments), h.Len())
	}

	for i := range segments {
		seg := h.Pop()
		if seg.sn != segments[i].sn {
			t.Errorf("expected seq %d, got %d", segments[i].sn, seg.sn)
		}
	}
}

// BenchmarkDebugLog measures DebugLog overhead with debug logging enabled or
// disabled via build tags.
// trace log on:
//
//	go test -benchmem -run=^$ -bench ^BenchmarkDebugLog$ -tags debug
//
// trace log off:
func TestSetMtuBoundary(t *testing.T) {
	kcp := NewKCP(0, func(buf []byte, size int) {})

	tests := []struct {
		name string
		mtu  int
		want int // 0 = success, -1 = failure
	}{
		{"negative", -1, -1},
		{"zero", 0, -1},
		{"below overhead", IKCP_OVERHEAD - 1, -1},
		{"equal overhead", IKCP_OVERHEAD, -1},
		{"one above overhead (minimum valid)", IKCP_OVERHEAD + 1, 0},
		{"typical MTU", 1400, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kcp.SetMtu(tt.mtu)
			if got != tt.want {
				t.Errorf("SetMtu(%d) = %d, want %d", tt.mtu, got, tt.want)
			}
		})
	}

	// verify mss is correctly derived after a valid SetMtu
	kcp.SetMtu(1400)
	if kcp.mss != 1400-IKCP_OVERHEAD {
		t.Errorf("mss = %d, want %d", kcp.mss, 1400-IKCP_OVERHEAD)
	}

	// minimum valid MTU yields mss = 1
	kcp.SetMtu(IKCP_OVERHEAD + 1)
	if kcp.mss != 1 {
		t.Errorf("mss = %d, want 1 for minimum valid MTU", kcp.mss)
	}
}

// go test -benchmem -run=^$ -bench ^BenchmarkDebugLog$
func BenchmarkDebugLog(b *testing.B) {
	kcp := &KCP{
		conv:    123,
		snd_wnd: 456,
	}
	kcp.log = slog.Debug

	for b.Loop() {
		// In release mode, this line of code will be completely 'erased' by the compiler,
		// as if it doesn't exist at all, and even the parameter's interface conversion will not occur.
		kcp.debugLog(IKCP_LOG_OUT_WASK, "conv", kcp.conv, "wnd", kcp.snd_wnd)
	}
}

func TestSetMtuRetainsBufferForQueuedOldMssSegment(t *testing.T) {
	tests := []struct {
		name  string
		queue bool
	}{
		{name: "in-flight"},
		{name: "pending", queue: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var packets [][]byte
			kcp := NewKCP(1, func(buf []byte, size int) {
				if size > 0 {
					packets = append(packets, append([]byte(nil), buf[:size]...))
				}
			})
			oldData := make([]byte, 1000)
			segment := segment{cmd: IKCP_CMD_PUSH, xmit: 1, resendts: 0, data: oldData}
			if tt.queue {
				kcp.snd_queue.Push(segment)
				kcp.cwnd = 1
			} else {
				segment.sn = 0
				kcp.snd_buf.Push(segment)
				kcp.snd_nxt = 1
			}

			if got := kcp.SetMtu(25); got != 0 {
				t.Fatalf("SetMtu(25) = %d, want 0", got)
			}
			kcp.flush(IKCP_FLUSH_FULL)

			if len(packets) != 1 {
				t.Fatalf("got %d output packets, want 1", len(packets))
			}
			if got, want := len(packets[0]), IKCP_OVERHEAD+len(oldData); got != want {
				t.Fatalf("output packet size = %d, want %d", got, want)
			}
		})
	}
}

func TestKCPSendVector(t *testing.T) {
	t.Run("message mode multi-fragment", func(t *testing.T) {
		var wire [][]byte
		a := NewKCP(7, func(buf []byte, size int) {
			wire = append(wire, append([]byte(nil), buf[:size]...))
		})
		b := NewKCP(7, func(buf []byte, size int) {})
		a.SetMtu(100)
		b.SetMtu(100)
		a.NoDelay(1, 10, 2, 1)
		b.NoDelay(1, 10, 2, 1)

		hdr := []byte{0x03}
		body := bytes.Repeat([]byte("abcdef0123"), 20) // 200 bytes -> 3 fragments at mss=76
		if ret := a.SendVector([][]byte{nil, hdr, {}, body}); ret != 0 {
			t.Fatalf("SendVector = %d, want 0", ret)
		}
		a.flush(IKCP_FLUSH_FULL)

		for _, pkt := range wire {
			if ret := b.Input(pkt, IKCP_PACKET_REGULAR, false); ret != 0 {
				t.Fatalf("Input = %d, want 0", ret)
			}
		}

		want := append(append([]byte(nil), hdr...), body...)
		if got := b.PeekSize(); got != len(want) {
			t.Fatalf("PeekSize = %d, want %d", got, len(want))
		}
		out := make([]byte, len(want))
		if n := b.Recv(out); n != len(want) {
			t.Fatalf("Recv = %d, want %d", n, len(want))
		}
		if !bytes.Equal(out, want) {
			t.Fatalf("payload mismatch")
		}
	})

	t.Run("stream mode coalescing", func(t *testing.T) {
		a := NewKCP(9, func(buf []byte, size int) {})
		a.SetFramingMode(FramingStream)
		a.SetMtu(100) // mss = 76
		if ret := a.SendVector([][]byte{[]byte("hello "), []byte("world")}); ret != 0 {
			t.Fatalf("first SendVector = %d", ret)
		}
		if ret := a.SendVector([][]byte{[]byte("!"), []byte("!!")}); ret != 0 {
			t.Fatalf("second SendVector = %d", ret)
		}
		if a.snd_queue.Len() != 1 {
			t.Fatalf("snd_queue.Len() = %d, want 1", a.snd_queue.Len())
		}
		seg, _ := a.snd_queue.Peek()
		if string(seg.data) != "hello world!!!" {
			t.Fatalf("coalesced data = %q, want %q", string(seg.data), "hello world!!!")
		}
	})

	t.Run("empty and oversized", func(t *testing.T) {
		a := NewKCP(1, func(buf []byte, size int) {})
		if ret := a.SendVector(nil); ret != -1 {
			t.Fatalf("empty SendVector = %d, want -1", ret)
		}
		if ret := a.SendVector([][]byte{{}, {}}); ret != -1 {
			t.Fatalf("all-empty SendVector = %d, want -1", ret)
		}
		tooBig := make([]byte, a.MSS()*IKCP_FRG_MAX+1)
		if ret := a.SendVector([][]byte{tooBig}); ret != -2 {
			t.Fatalf("oversized SendVector = %d, want -2", ret)
		}

		// Stream mode is not constrained by IKCP_FRG_MAX and does not mutate input slices.
		a.SetFramingMode(FramingStream)
		part1 := []byte("prefix-")
		part2 := make([]byte, a.MSS()*(IKCP_FRG_MAX+5))
		for i := range part2 {
			part2[i] = byte(i)
		}
		vec := [][]byte{part1, part2}
		if ret := a.SendVector(vec); ret != 0 {
			t.Fatalf("stream mode large SendVector = %d, want 0", ret)
		}
		if len(vec[0]) != len(part1) || len(vec[1]) != len(part2) {
			t.Fatal("SendVector mutated caller slice headers")
		}
		for seg := range a.snd_queue.ForEach {
			if seg.frg != 0 {
				t.Fatalf("stream mode seg.frg = %d, want 0", seg.frg)
			}
		}
	})
}

func BenchmarkWriteBuffersStreamVector(b *testing.B) {
	s := &UDPSession{
		shutdown:   make(chan struct{}),
		writeDelay: true,
		kcp:        NewKCP(1, func(buf []byte, size int) {}),
	}
	s.kcp.SetFramingMode(FramingStream)
	s.kcp.WndSize(1024, 1024)
	v := [][]byte{
		make([]byte, 16),
		make([]byte, 64),
		make([]byte, 128),
		make([]byte, 256),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.writeBuffersLocked(v)
		for {
			seg, ok := s.kcp.snd_queue.Pop()
			if !ok {
				break
			}
			s.kcp.recycleSegment(&seg)
		}
	}
}

func BenchmarkSendVectorLargePrealloc(b *testing.B) {
	payload := make([]byte, 512*1024)
	v := [][]byte{payload[:128], payload[128:]}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		k := NewKCP(1, func(buf []byte, size int) {})
		k.SetFramingMode(FramingStream)
		k.SendVector(v)
		for {
			seg, ok := k.snd_queue.Pop()
			if !ok {
				break
			}
			k.recycleSegment(&seg)
		}
	}
}
