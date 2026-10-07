package kcp

import (
	"math/rand"
	"sort"
	"testing"
	"time"
)

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
