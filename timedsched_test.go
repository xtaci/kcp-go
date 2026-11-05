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
	for range 8 {
		go func() {
			for i := range n / 8 {
				deadline := time.Now().Add(time.Duration(i%50) * time.Millisecond)
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
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("only %d of %d tasks ran", runs.Load(), n)
	}
	if runs.Load() != n || early.Load() != 0 || late.Load() != 0 {
		t.Fatalf("runs %d of %d, %d early, %d late", runs.Load(), n, early.Load(), late.Load())
	}
}

// A task that puts itself again, as UDPSession.update does.
func TestTimedSchedSelfReschedule(t *testing.T) {
	ts := NewTimedSched(1)
	defer ts.Close()

	const (
		ticks    = 50
		interval = 2 * schedSlack
	)
	var count atomic.Int64
	done := make(chan struct{})
	var tick func()
	tick = func() {
		if count.Add(1) == ticks {
			close(done)
			return
		}
		ts.Put(tick, time.Now().Add(interval))
	}
	start := time.Now()
	ts.Put(tick, time.Now())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("only %d runs", count.Load())
	}
	if elapsed := time.Since(start); elapsed < (ticks-1)*(interval-schedSlack) {
		t.Fatalf("%d ticks of %v took only %v", ticks, interval, elapsed)
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
	for i := range 1000 {
		ts := base.Add(time.Duration((i*37)%500) * time.Millisecond)
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
	for i := range 512 {
		h.push(timedFunc{f, base.Add(time.Duration(i) * time.Microsecond)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		x := h.pop()
		x.ts = x.ts.Add(time.Millisecond)
		h.push(x)
	}
}
