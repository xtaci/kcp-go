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
	"container/heap"
	"io"
	"log/slog"
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
			client, err := lossyconn.NewWithConfig(clientConfig)
			if err != nil {
				t.Fatal(err)
			}
			server, err := lossyconn.NewWithConfig(serverConfig)
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
	serverSessions := make(chan *UDPSession, 1)
	go func() {
		conn, err := listener.AcceptKCP()
		if err != nil {
			return
		}
		serverSessions <- conn
		if err := conn.SetDeadline(time.Now().Add(lossyTestTimeout)); err != nil {
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
					t.Errorf("echo server read: %v", err)
				}
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				select {
				case <-done:
				default:
					t.Errorf("echo server write: %v", err)
				}
				return
			}
		}
	}()

	defer func() {
		close(done)
		select {
		case conn := <-serverSessions:
			_ = conn.Close()
		default:
		}
		_ = listener.Close()
		_ = sess.Close()
		_ = client.Close()
		_ = server.Close()
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
		heap.Push(h, seg)
		t.Logf("pushed segment with seq %d", seg.sn)
	}

	if h.Len() != len(segments) {
		t.Errorf("expected length %d, got %d", len(segments), h.Len())
	}

	for i := range segments {
		seg := heap.Pop(h).(segment)
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
