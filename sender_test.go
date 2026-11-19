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
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/transport/v5/deadline"
	libsync "github.com/xtaci/kcp-go/v5/internal/lib/sync"
)

type blockingPacketConn struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	writes  atomic.Int32
}

func (c *blockingPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, net.ErrClosed
}

func (c *blockingPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.writes.Add(1)
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return len(p), nil
}

func (c *blockingPacketConn) Close() error                     { return nil }
func (c *blockingPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{Port: 1} }
func (c *blockingPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *blockingPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *blockingPacketConn) SetWriteDeadline(time.Time) error { return nil }

type recordingPacketConn struct {
	mutex   sync.Mutex
	packets [][]byte
}

func (c *recordingPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, net.ErrClosed
}

func (c *recordingPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.mutex.Lock()
	c.packets = append(c.packets, bytes.Clone(p))
	c.mutex.Unlock()
	return len(p), nil
}

func (c *recordingPacketConn) Close() error                     { return nil }
func (c *recordingPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{Port: 1} }
func (c *recordingPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingPacketConn) SetWriteDeadline(time.Time) error { return nil }

type blockingBlockCrypt struct {
	entered chan struct{}
	release chan struct{}
}

func (c *blockingBlockCrypt) Encrypt(dst, src []byte) {
	close(c.entered)
	<-c.release
	copy(dst, src)
}

func (c *blockingBlockCrypt) Decrypt(dst, src []byte) { copy(dst, src) }

func TestSenderSetDUP(t *testing.T) {
	for _, tt := range []struct {
		name string
		dup  int
		want int
	}{
		{name: "negative", dup: -1, want: 1},
		{name: "zero", dup: 0, want: 1},
		{name: "multiple", dup: 3, want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn := &recordingPacketConn{}
			s := &UDPSession{
				conn:       conn,
				remoteAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
				shutdown:   libsync.NewQuit(),
			}
			s.initPlatform()
			s.SetDUP(tt.dup)

			buf := defaultBufferPool.Get()[:IKCP_OVERHEAD]
			clear(buf)
			buf[0] = 42
			if !s.sender.queue.push(sendRequest{buffer: buf}) {
				defaultBufferPool.Put(buf)
				t.Fatal("failed to queue packet")
			}
			s.postProcess(&txWorker{})

			if len(conn.packets) != tt.want {
				t.Fatalf("sent %d packets, want %d", len(conn.packets), tt.want)
			}
			for _, packet := range conn.packets {
				if len(packet) != IKCP_OVERHEAD || packet[0] != 42 {
					t.Fatalf("unexpected packet: %x", packet)
				}
			}
		})
	}
}

func TestSenderConcurrentSetDUP(t *testing.T) {
	conn := &recordingPacketConn{}
	s := &UDPSession{
		conn:       conn,
		remoteAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
		shutdown:   libsync.NewQuit(),
	}
	s.initPlatform()

	const packets = 512
	for range packets {
		buf := defaultBufferPool.Get()[:IKCP_OVERHEAD]
		if !s.sender.queue.push(sendRequest{buffer: buf}) {
			defaultBufferPool.Put(buf)
			t.Fatal("failed to queue packet")
		}
	}

	started := make(chan struct{})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.SetDUP(0)
		close(started)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				s.SetDUP(i % 2)
			}
		}
	}()
	<-started

	w := &txWorker{}
	for s.postProcess(w) {
	}
	close(stop)
	<-done

	if got := len(conn.packets); got < packets || got > 2*packets {
		t.Fatalf("sent %d packets, want between %d and %d", got, packets, 2*packets)
	}
}

// TestSenderWorkerHandoff verifies that queued work has exactly one worker claim.
func TestSenderWorkerHandoff(t *testing.T) {
	for i := range 1000 {
		s := &UDPSession{
			shutdown:      libsync.NewQuit(),
			readDeadline:  deadline.New(),
			writeDeadline: deadline.New(),
			sender: sender{
				postProcessCalls: libsync.NewQuit(),
			},
		}

		s.sender.queue.push(sendRequest{})
		s.sender.running.Store(true)

		start := make(chan struct{})
		var wg sync.WaitGroup
		var claims atomic.Int32
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if s.claimSender() {
				claims.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if s.finishSender() {
				claims.Add(1)
			}
		}()
		close(start)
		wg.Wait()

		if got := claims.Load(); got != 1 {
			t.Fatalf("iteration %d: got %d scheduling claims, want exactly one", i, got)
		}
		if !s.sender.running.Load() {
			t.Fatalf("iteration %d: pending packet has no worker scheduled", i)
		}
	}
}

// TestCloseWaitsForActiveSenderSend verifies that closing waits for in-flight writes.
func TestCloseWaitsForActiveSenderSend(t *testing.T) {
	conn := &blockingPacketConn{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	s := &UDPSession{
		conn:          conn,
		shutdown:      libsync.NewQuit(),
		kcp:           NewKCP(1, nil),
		readDeadline:  deadline.New(),
		writeDeadline: deadline.New(),
		sender: sender{
			postProcessCalls: libsync.NewQuit(),
		},
		closeGate: make(chan struct{}, 1),
	}
	s.closeGate <- struct{}{}
	s.output(make([]byte, IKCP_OVERHEAD), IKCP_OVERHEAD)

	select {
	case <-conn.entered:
	case <-time.After(time.Second):
		close(conn.release)
		t.Fatal("sender did not start writing")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- s.Close() }()
	select {
	case <-s.shutdown.Signal():
	case <-time.After(time.Second):
		close(conn.release)
		t.Fatal("Close did not close the session")
	}
	select {
	case err := <-closeDone:
		close(conn.release)
		t.Fatalf("Close returned before active send completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	secondCloseDone := make(chan error, 1)
	go func() { secondCloseDone <- s.Close() }()
	select {
	case err := <-secondCloseDone:
		close(conn.release)
		t.Fatalf("concurrent Close returned before active send completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(conn.release)
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondCloseDone; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("concurrent Close returned %v, want ErrClosedPipe", err)
	}

	if s.sender.postProcessCalls.Add(1) {
		s.sender.postProcessCalls.Done()
		t.Fatal("sender admitted work after Close")
	}
	if got := conn.writes.Load(); got != 1 {
		t.Fatalf("got %d writes, want exactly 1", got)
	}
}

// TestFinishClosedSenderLeavesQueue verifies that Close owns queue cleanup.
func TestFinishClosedSenderLeavesQueue(t *testing.T) {
	s := &UDPSession{
		shutdown:      libsync.NewQuit(),
		readDeadline:  deadline.New(),
		writeDeadline: deadline.New(),
		sender: sender{
			postProcessCalls: libsync.NewQuit(),
		},
	}

	s.shutdown.Wait()
	s.sender.running.Store(true)
	if !s.sender.queue.push(sendRequest{buffer: defaultBufferPool.Get()}) {
		t.Fatal("failed to queue packet")
	}

	if s.finishSender() {
		t.Fatal("closed session was scheduled for more post-processing")
	}
	req, ok := s.sender.queue.pop()
	if !ok {
		t.Fatal("worker discarded a packet reserved for the close drain")
	}
	defaultBufferPool.Put(req.buffer)
}

func TestCloseDiscardsDelayedAndQueuedPackets(t *testing.T) {
	conn := &recordingPacketConn{}
	s := &UDPSession{
		conn:          conn,
		remoteAddr:    &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
		shutdown:      libsync.NewQuit(),
		readDeadline:  deadline.New(),
		writeDeadline: deadline.New(),
		writeErrorCh:  make(chan struct{}),
		writeGate:     make(chan struct{}, 1),
		closeGate:     make(chan struct{}, 1),
		writeDelay:    true,
		sender:        sender{postProcessCalls: libsync.NewQuit()},
	}
	s.writeGate <- struct{}{}
	s.closeGate <- struct{}{}
	s.kcp = NewKCP(1, s.output)
	s.initPlatform()
	DefaultSnmp.increaseCurrEstab()

	if _, err := s.Write([]byte("delayed data")); err != nil {
		t.Fatal(err)
	}
	for range devBacklog {
		if !s.sender.queue.push(sendRequest{buffer: defaultBufferPool.Get()[:IKCP_OVERHEAD]}) {
			t.Fatal("failed to fill the send queue")
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	if got := len(conn.packets); got != 0 {
		t.Fatalf("Close sent %d queued packets", got)
	}
	if got := s.sender.queue.len(); got != 0 {
		t.Fatalf("Close retained %d queued packets", got)
	}
}

func TestCloseWaitsForPacketBeingProcessed(t *testing.T) {
	conn := &recordingPacketConn{}
	block := &blockingBlockCrypt{entered: make(chan struct{}), release: make(chan struct{})}
	s, err := NewConn3(1, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, block, 2, 1, conn)
	if err != nil {
		t.Fatal(err)
	}
	s.mutex.Lock()
	s.output(make([]byte, IKCP_OVERHEAD), IKCP_OVERHEAD)
	s.mutex.Unlock()
	select {
	case <-block.entered:
	case <-time.After(time.Second):
		close(block.release)
		t.Fatal("sender did not start processing")
	}
	s.mutex.Lock()
	s.output(make([]byte, IKCP_OVERHEAD), IKCP_OVERHEAD)
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case <-s.shutdown.Signal():
	case <-time.After(time.Second):
		close(block.release)
		t.Fatal("Close did not start")
	}
	select {
	case err := <-done:
		close(block.release)
		t.Fatalf("Close returned before processing finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(block.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish")
	}

	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	if got := len(conn.packets); got != 0 {
		t.Fatalf("Close sent %d packets still being processed", got)
	}
	if got := s.sender.queue.len(); got != 0 {
		t.Fatalf("Close retained %d queued packets", got)
	}
}

func TestCloseRejectsWriterWaitingForSessionLock(t *testing.T) {
	conn := &recordingPacketConn{}
	s, err := NewConn3(1, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil, 0, 0, conn)
	if err != nil {
		t.Fatal(err)
	}

	s.mutex.Lock()
	started := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		close(started)
		_, ok := s.writeBuffers([][]byte{[]byte("late write")})
		done <- ok
	}()
	<-started
	s.shutdown.Wait()
	s.mutex.Unlock()
	if <-done {
		t.Fatal("write succeeded after the close signal")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// Writes waiting for application-rate tokens must not hold shared
// postProcess workers from an unlimited session.
func TestSenderRateLimitedMixed(t *testing.T) {
	port := nextPort()
	l := sinkServer(port)
	defer l.Close()

	// More limited sessions than workers, each with more queued than its
	// limit lets out.
	stop := make(chan struct{})
	defer close(stop)
	limited := make([]*UDPSession, 0, 16)
	for range 16 {
		s, err := dialSink(port)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.SetRateLimit(32 * 1024)
		limited = append(limited, s)
		go func() {
			buf := make([]byte, 64*1024)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.Write(buf); err != nil {
					return
				}
			}
		}()
	}
	waitForRateLimiters(t, limited)

	eport := nextPort()
	el := echoServer(eport, nil)
	defer el.Close()
	cli, err := dialEcho(eport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	cli.SetRateLimit(0)
	cli.SetLogger(0, nil)

	start := time.Now()
	if err := echo_tester(cli, 1024, 256); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("unlimited session echoed 256 KiB in %v", took)
	if took > 2*time.Second {
		t.Fatalf("unlimited session took %v behind rate limited ones", took)
	}
}

func waitForRateLimiters(t *testing.T, sessions []*UDPSession) {
	t.Helper()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		waiting := true
		for _, session := range sessions {
			bucket := session.bucket.Load()
			if bucket == nil || bucket.Available() >= 0 {
				waiting = false
				break
			}
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("rate limited sessions did not exhaust their bursts")
		}
	}
}
