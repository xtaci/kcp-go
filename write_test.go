package kcp

import (
	"sync/atomic"
	"testing"
)

// An empty write must not flush, since some callers probe the conn with it.
func TestEmptyWriteDoesNotFlush(t *testing.T) {
	var outputs atomic.Int64
	s := new(UDPSession)
	s.die = make(chan struct{})
	s.chSocketWriteError = make(chan struct{})
	s.kcp = NewKCP(1, func([]byte, int) { outputs.Add(1) })
	// A queued segment that any flush would send.
	s.kcp.Send([]byte("x"))
	before := outputs.Load()
	for i := 0; i < 10; i++ {
		if n, err := s.Write(nil); n != 0 || err != nil {
			t.Fatalf("Write(nil) = %d, %v", n, err)
		}
	}
	if got := outputs.Load(); got != before {
		t.Fatalf("Write(nil) flushed %d times", got-before)
	}
	close(s.die)
	if _, err := s.Write(nil); err == nil {
		t.Fatal("Write(nil) on a closed session returned no error")
	}
}
