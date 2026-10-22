package kcp

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTimedSchedRunsEachTaskOnTime(t *testing.T) {
	ts := NewTimedSched(2)
	defer ts.Close()

	const n = 2000
	var early, late, runs atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for g := 0; g < 8; g++ {
		go func() {
			for i := 0; i < n/8; i++ {
				deadline := time.Now().Add(time.Duration(rand.Intn(50)) * time.Millisecond)
				ts.Put(func() {
					d := time.Since(deadline)
					if d < -schedSlack {
						early.Add(1)
					} else if d > 200*time.Millisecond {
						late.Add(1)
					}
					runs.Add(1)
					wg.Done()
				}, deadline)
			}
		}()
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	if runs.Load() != n || early.Load() != 0 || late.Load() != 0 {
		t.Fatalf("runs %d of %d, %d early, %d late", runs.Load(), n, early.Load(), late.Load())
	}
}

// A task that puts itself again, as UDPSession.update does.
func TestTimedSchedSelfReschedule(t *testing.T) {
	ts := NewTimedSched(1)
	defer ts.Close()

	var count atomic.Int64
	done := make(chan struct{})
	var tick func()
	tick = func() {
		if count.Add(1) == 50 {
			close(done)
			return
		}
		ts.Put(tick, time.Now().Add(2*time.Millisecond))
	}
	start := time.Now()
	ts.Put(tick, time.Now())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("only %d runs", count.Load())
	}
	if el := time.Since(start); el < 50*2*time.Millisecond-50*schedSlack {
		t.Fatalf("50 ticks of 2ms took only %v", el)
	}
}

// A task put for later wakes a shard that is sleeping until much later.
func TestTimedSchedEarlierTaskWakes(t *testing.T) {
	ts := NewTimedSched(1)
	defer ts.Close()

	ts.Put(func() {}, time.Now().Add(time.Hour))
	time.Sleep(10 * time.Millisecond)
	ran := make(chan time.Duration, 1)
	start := time.Now()
	ts.Put(func() { ran <- time.Since(start) }, start.Add(20*time.Millisecond))
	select {
	case d := <-ran:
		if d > 500*time.Millisecond {
			t.Fatalf("ran after %v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("earlier task never ran")
	}
}
