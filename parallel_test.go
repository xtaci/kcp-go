package kcp

import (
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func inc(hp *hostParallel, count int) {
	for i := 0; i < int(math.Abs(float64(count))); i++ {
		if count > 0 {
			hp.inc()
		} else {
			hp.dec()
		}
	}
}

func incParallel(hp *hostParallel, count int) {
	for i := 0; i < count; i++ {
		hp.incParallel()
	}
}

func TestHostParallel(t *testing.T) {
	extraCachePeriods = 2

	pc := newParallelCtrl(2, 0.2, time.Second)
	hp := pc.getHostParallel("host1")

	hp.reset()

	inc(hp, 110)
	inc(hp, -10)

	if atomic.LoadInt64(&hp.streams) != 100 {
		t.Fatal("streams count wrong")
	}

	incParallel(hp, 5)
	time.Sleep(time.Second)
	incParallel(hp, 5)
	time.Sleep(time.Second)
	incParallel(hp, 5)
	time.Sleep(time.Second)
	incParallel(hp, 5)

	count := atomic.LoadInt64(&hp.count)
	if count > 15 {
		t.Fatal("parallel count")
	}
	parallel := hp.isParallel()
	if parallel {
		t.Fatal("parallel is true")
	}

	incParallel(hp, 15)
	parallel = hp.isParallel()
	if !parallel {
		t.Fatal("parallel is not true")
	}

	time.Sleep(time.Second * 2)
	parallel = hp.isParallel()
	if parallel {
		t.Fatal("parallel is true")
	}

	Logf(INFO, "ring count test")

	for i := 0; i < 10; i++ {
		incParallel(hp, 5)
		time.Sleep(time.Second)

		count := atomic.LoadInt64(&hp.count)
		if count > 20 {
			t.Fatal("parallel count")
		}
	}
}
