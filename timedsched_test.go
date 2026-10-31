package kcp

import (
	"math/rand"
	"sort"
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

// A shard delayed past schedSlack between popping due tasks and arming its
// timer finds its head already due. It must still wake for it.
func TestTimedSchedArmsWhenHeadPassedWhileArming(t *testing.T) {
	var once sync.Once
	hook := func() { once.Do(func() { time.Sleep(4 * schedSlack) }) }
	testHookBeforeArm.Store(&hook)
	defer testHookBeforeArm.Store(nil)

	ts := NewTimedSched(1)
	defer ts.Close()
	time.Sleep(50 * time.Millisecond) // let the shard park with nothing queued
	ran := make(chan struct{})
	ts.Put(func() { close(ran) }, time.Now().Add(2*schedSlack))
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("task never ran: the shard slept with no timer armed")
	}
}

func TestTimedFuncHeapOrder(t *testing.T) {
	base := time.Now()
	var h timedFuncHeap
	var want []time.Time
	for i := 0; i < 1000; i++ {
		ts := base.Add(time.Duration(rand.Intn(500)) * time.Millisecond)
		want = append(want, ts)
		h.push(timedFunc{func() {}, ts})
		if i%7 == 0 {
			sort.Slice(want, func(a, b int) bool { return want[a].Before(want[b]) })
			if got := h.pop().ts; !got.Equal(want[0]) {
				t.Fatalf("pop %d: got %v, want %v", i, got, want[0])
			}
			want = want[1:]
		}
	}
	sort.Slice(want, func(a, b int) bool { return want[a].Before(want[b]) })
	for i, w := range want {
		if got := h.pop().ts; !got.Equal(w) {
			t.Fatalf("drain %d: got %v, want %v", i, got, w)
		}
	}
	if h.Len() != 0 {
		t.Fatalf("heap not empty: %d", h.Len())
	}
}

func BenchmarkTimedFuncHeap(b *testing.B) {
	base := time.Now()
	h := make(timedFuncHeap, 0, 1024)
	f := func() {}
	for i := 0; i < 512; i++ {
		h.push(timedFunc{f, base.Add(time.Duration(i) * time.Microsecond)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x := h.pop()
		x.ts = x.ts.Add(time.Millisecond)
		h.push(x)
	}
}
