package kcp

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/time/rate"
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
	for i := 0; i < 1000; i++ {
		s := &UDPSession{}
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
		conn: conn,
		die:  make(chan struct{}),
		kcp:  NewKCP(1, nil),
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

func TestDiscardHeldPostProcessCancelsReservation(t *testing.T) {
	s := &UDPSession{}
	held := &ppWorker{
		txqueue: []ipv4.Message{{Buffers: [][]byte{defaultBufferPool.Get()}}},
		bufs:    make([][]byte, 1),
	}
	held.bufs[0] = held.txqueue[0].Buffers[0]
	limiter := rate.NewLimiter(1, 1)
	now := time.Now()
	if !limiter.AllowN(now, 1) {
		t.Fatal("failed to consume initial burst")
	}
	reservation := limiter.ReserveN(now, 1)
	if !reservation.OK() || reservation.DelayFrom(now) <= 0 {
		t.Fatal("expected a delayed rate-limit reservation")
	}
	if !s.setHeldPostProcess(held, len(held.txqueue[0].Buffers[0]), now.Add(reservation.DelayFrom(now)), reservation) {
		t.Fatal("failed to hold batch")
	}
	_, wake, scheduled := s.heldPostProcessUntil()
	if !scheduled || wake.session.Load() != s {
		t.Fatal("held batch did not retain its scheduled session")
	}

	s.discardHeldPostProcess()

	if w, _, _, _ := s.takeHeldPostProcess(); w != nil {
		t.Fatal("held batch was not released")
	}
	if wake.session.Load() != nil {
		t.Fatal("discarded timer still retains the session")
	}
	if tokens := limiter.TokensAt(now); tokens < -0.01 {
		t.Fatalf("cancelled reservation left %f tokens, want at least 0", tokens)
	}
}

func TestFinishClosedPostProcessDrainsQueue(t *testing.T) {
	s := &UDPSession{die: make(chan struct{})}
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

// Rate limited sessions waiting for tokens must not hold the shared
// postProcess workers from a session without a limit.
func TestPostProcessRateLimitedMixed(t *testing.T) {
	port := nextPort()
	l := sinkServer(port)
	defer l.Close()

	// More limited sessions than workers, each with more queued than its
	// limit lets out.
	stop := make(chan struct{})
	defer close(stop)
	limited := make([]*UDPSession, 0, 16)
	for i := 0; i < 16; i++ {
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
			limiter := session.rateLimiter.Load()
			if limiter == nil || limiter.Tokens() >= 0 {
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
