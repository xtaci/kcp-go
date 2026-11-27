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
	"fmt"
	"testing"
	"time"

	"github.com/pion/transport/v5/deadline"
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
	writeDone := make(chan error, 1)
	go func() {
		_, err := sess.Write(want)
		writeDone <- err
	}()

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
	if err := <-writeDone; err != nil {
		t.Fatal(err)
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
		shutdown:     make(chan struct{}),
		readEvent:    make(chan struct{}, 1),
		readErrorCh:  make(chan struct{}),
		readDeadline: deadline.New(),
		kcp:          NewKCP(1, nil),
	}
	s.input.Reset([]byte("ab"))
	defer close(s.shutdown)

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

func TestFramingModes(t *testing.T) {
	newSession := func() *UDPSession {
		s := &UDPSession{
			shutdown:   make(chan struct{}),
			writeDelay: true,
			kcp:        NewKCP(1, func(buf []byte, size int) {}),
		}
		s.SetFramingMode(FramingStream)
		s.kcp.SetMtu(100) // mss = 76
		return s
	}

	t.Run("defaults: NewKCP uses FramingMessage, newUDPSession uses FramingStream", func(t *testing.T) {
		k := NewKCP(1, func(buf []byte, size int) {})
		if k.framing != FramingMessage {
			t.Fatalf("NewKCP framing = %v, want FramingMessage", k.framing)
		}

		port := nextPort()
		l, err := ListenWithOptions(fmt.Sprintf("127.0.0.1:%d", port), nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()

		cli, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%d", port), nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer cli.Close()
		if cli.kcp.framing != FramingStream {
			t.Fatalf("DialWithOptions framing = %v, want FramingStream", cli.kcp.framing)
		}
	})

	t.Run("FramingStream packs vector, coalesces across writes, allows large writes, and maps SetStreamMode", func(t *testing.T) {
		s := newSession()
		if s.kcp.framing != FramingStream {
			t.Fatalf("framing = %v, want FramingStream", s.kcp.framing)
		}
		s.writeBuffersLocked([][]byte{[]byte("ab"), []byte("cd")})
		s.writeBuffersLocked([][]byte{[]byte("ef")})
		if s.kcp.snd_queue.Len() != 1 {
			t.Fatalf("snd_queue.Len() = %d, want 1", s.kcp.snd_queue.Len())
		}
		if got := string(s.kcp.snd_queue.At(0).data); got != "abcdef" {
			t.Fatalf("coalesced = %q, want %q", got, "abcdef")
		}

		// Oversized write (> IKCP_FRG_MAX segments) succeeds with frg=0.
		s2 := newSession()
		s2.kcp.WndSize(512, 512)
		large := make([]byte, s2.kcp.MSS()*(IKCP_FRG_MAX+3))
		if n, ok, err := s2.writeBuffersLocked([][]byte{{0x01}, large}); err != nil || !ok || n != 1+len(large) {
			t.Fatalf("large stream write = (%d, %v, %v), want (%d, true, nil)", n, ok, err, 1+len(large))
		}
		for seg := range s2.kcp.snd_queue.ForEach {
			if seg.frg != 0 {
				t.Fatalf("stream seg.frg = %d, want 0", seg.frg)
			}
		}

		// Invalid mode is ignored.
		s.SetFramingMode(FramingMode(99))
		if s.kcp.framing != FramingStream {
			t.Fatalf("invalid mode mutated state: framing=%v", s.kcp.framing)
		}

		// Deprecated SetStreamMode always sets FramingStream regardless of enable.
		s.SetFramingMode(FramingMessage)
		s.SetStreamMode(false)
		if s.kcp.framing != FramingStream {
			t.Fatalf("SetStreamMode(false) -> framing=%v, want FramingStream", s.kcp.framing)
		}
		s.SetStreamMode(true)
		if s.kcp.framing != FramingStream {
			t.Fatalf("SetStreamMode(true) -> framing=%v, want FramingStream", s.kcp.framing)
		}
	})

	t.Run("FramingMessage sets frg and rejects oversized writes", func(t *testing.T) {
		s := newSession()
		s.SetFramingMode(FramingMessage)
		if s.kcp.framing != FramingMessage {
			t.Fatalf("framing = %v, want FramingMessage", s.kcp.framing)
		}
		// 200 bytes at mss=76 -> 3 fragments with frg 2, 1, 0.
		body := make([]byte, 200)
		if n, ok, err := s.writeBuffersLocked([][]byte{body[:10], body[10:]}); err != nil || !ok || n != 200 {
			t.Fatalf("message write = (%d, %v, %v), want (200, true, nil)", n, ok, err)
		}
		if s.kcp.snd_queue.Len() != 3 {
			t.Fatalf("snd_queue.Len() = %d, want 3", s.kcp.snd_queue.Len())
		}
		for i, wantFrg := range []uint8{2, 1, 0} {
			if got := s.kcp.snd_queue.At(i).frg; got != wantFrg {
				t.Fatalf("seg[%d].frg = %d, want %d", i, got, wantFrg)
			}
		}

		oversized := make([]byte, s.kcp.MSS()*IKCP_FRG_MAX+1)
		if _, ok, err := s.writeBuffersLocked([][]byte{oversized}); ok || !errors.Is(err, ErrMessageTooLarge) {
			t.Fatalf("oversized message write = (ok=%v, err=%v), want (false, ErrMessageTooLarge)", ok, err)
		}
	})
}
