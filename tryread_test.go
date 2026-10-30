package kcp

import (
	"bytes"
	"testing"
	"time"
)

// A consumer can read a session through SetReadNotify and TryRead alone.
func TestTryReadWithNotify(t *testing.T) {
	port := nextPort()
	l := echoServer(port, nil)
	defer l.Close()
	sess, err := dialEcho(port, nil)
	if err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{}, 1)
	sess.SetReadNotify(func() {
		select {
		case ready <- struct{}{}:
		default:
		}
	})

	want := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	go func() { _, _ = sess.Write(want) }()

	var got []byte
	buf := make([]byte, 1500)
	deadline := time.After(10 * time.Second)
	for len(got) < len(want) {
		n, err := sess.TryRead(buf)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			got = append(got, buf[:n]...)
			continue
		}
		select {
		case <-ready:
		case <-deadline:
			t.Fatalf("got %d of %d bytes", len(got), len(want))
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatal("echo mismatch")
	}

	sess.Close()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("Close did not notify")
	}
	// Either the close itself or the closed socket's read error.
	if _, err := sess.TryRead(buf); err == nil {
		t.Fatalf("TryRead after Close: %v", err)
	}
}

func TestConcurrentReadersNotifiedForBufferedData(t *testing.T) {
	s := &UDPSession{
		die:               make(chan struct{}),
		chReadEvent:       make(chan struct{}, 1),
		chSocketReadError: make(chan struct{}),
		kcp:               NewKCP(1, nil),
		bufptr:            []byte("ab"),
	}
	defer close(s.die)

	type result struct {
		value byte
		err   error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			var b [1]byte
			_, err := s.Read(b[:])
			results <- result{value: b[0], err: err}
		}()
	}
	close(start)
	s.notifyReadEvent()

	var seen [256]bool
	for range 2 {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatal(r.err)
			}
			seen[r.value] = true
		case <-time.After(time.Second):
			t.Fatal("a reader remained blocked while buffered data was available")
		}
	}
	if !seen['a'] || !seen['b'] {
		t.Fatalf("read bytes a=%t b=%t, want both", seen['a'], seen['b'])
	}
}
