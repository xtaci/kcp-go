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
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/transport/v5/deadline"
	"golang.org/x/net/ipv4"
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

func TestPostProcessWorkerHandoff(t *testing.T) {
	for i := range 1000 {
		s := &UDPSession{
			readDeadline:  deadline.New(),
			writeDeadline: deadline.New(),
		}
		s.postQueue.push(sendRequest{})
		s.ppRunning.Store(true)

		start := make(chan struct{})
		var wg sync.WaitGroup
		var claims atomic.Int32
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if s.claimPostProcess() {
				claims.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if s.finishPostProcess() {
				claims.Add(1)
			}
		}()
		close(start)
		wg.Wait()

		if got := claims.Load(); got != 1 {
			t.Fatalf("iteration %d: got %d scheduling claims, want exactly one", i, got)
		}
		if !s.ppRunning.Load() {
			t.Fatalf("iteration %d: pending packet has no worker scheduled", i)
		}
	}
}

func TestCloseWaitsForActivePostProcessSend(t *testing.T) {
	conn := &blockingPacketConn{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	s := &UDPSession{
		conn:          conn,
		die:           make(chan struct{}),
		kcp:           NewKCP(1, nil),
		readDeadline:  deadline.New(),
		writeDeadline: deadline.New(),
	}
	w := &ppWorker{
		txqueue: []ipv4.Message{{
			Buffers: [][]byte{defaultBufferPool.Get()[:1]},
			Addr:    conn.LocalAddr(),
		}},
	}
	sendDone := make(chan struct{})
	go func() {
		s.sendTx(w, 1)
		close(sendDone)
	}()

	select {
	case <-conn.entered:
	case <-time.After(time.Second):
		close(conn.release)
		t.Fatal("sendTx did not start writing")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- s.Close() }()
	select {
	case <-s.die:
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
	select {
	case <-sendDone:
	case <-time.After(time.Second):
		t.Fatal("active send did not finish")
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondCloseDone; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("concurrent Close returned %v, want ErrClosedPipe", err)
	}

	afterClose := &ppWorker{
		txqueue: []ipv4.Message{{
			Buffers: [][]byte{defaultBufferPool.Get()[:1]},
			Addr:    conn.LocalAddr(),
		}},
	}
	if s.sendTx(afterClose, 1) {
		t.Fatal("sendTx succeeded after Close")
	}
	if got := conn.writes.Load(); got != 1 {
		t.Fatalf("got %d writes, want exactly 1", got)
	}
}

func TestFinishClosedPostProcessDrainsQueue(t *testing.T) {
	s := &UDPSession{
		die:           make(chan struct{}),
		readDeadline:  deadline.New(),
		writeDeadline: deadline.New(),
	}
	close(s.die)
	s.ppRunning.Store(true)
	if !s.postQueue.push(sendRequest{buffer: defaultBufferPool.Get()}) {
		t.Fatal("failed to queue packet")
	}

	if s.finishPostProcess() {
		t.Fatal("closed session was scheduled for more post-processing")
	}
	if got := s.postQueue.len(); got != 0 {
		t.Fatalf("closed session retained %d queued packets", got)
	}
}

// Writes waiting for application-rate tokens must not hold shared
// postProcess workers from an unlimited session.
func TestPostProcessRateLimitedMixed(t *testing.T) {
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
