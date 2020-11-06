package kcp

import (
	"sync"
	"sync/atomic"
	"time"
)

var (
	UpdateIntervalMs        = 250
	ExtraCachePeriods int64 = 10
)

type parallelCtrl struct {
	periods  int64
	rate     float64
	duration time.Duration
	hpm      map[string]*hostParallel
	mu       sync.RWMutex
}

func newParallelCtrl(periods int64, rate float64, duration time.Duration) *parallelCtrl {
	Logf(WARN, "newParallelCtrl periods:%v rate:%v duration:%v", periods, rate, duration)

	return &parallelCtrl{
		periods:  periods,
		rate:     rate,
		duration: duration,
		hpm:      make(map[string]*hostParallel),
	}
}

func (p *parallelCtrl) getHostParallel(host string) *hostParallel {
	p.mu.RLock()
	hp, ok := p.hpm[host]
	p.mu.RUnlock()

	if ok {
		return hp
	}

	p.mu.Lock()
	hp, ok = p.hpm[host]
	if !ok {
		hp = newHostParallel(host, p)
		p.hpm[host] = hp
	}
	p.mu.Unlock()
	return hp
}

type hostParallel struct {
	host        string
	p           *parallelCtrl
	ringCounter []int64
	count       int64
	streams     int64
	expire      int64
	lastDecT    int64
}

func newHostParallel(host string, p *parallelCtrl) *hostParallel {
	Logf(WARN, "newHostParallel host:%v", host)

	h := &hostParallel{
		host:        host,
		p:           p,
		ringCounter: make([]int64, p.periods+ExtraCachePeriods),
		lastDecT:    time.Now().Add(-time.Duration(p.periods) * time.Second).Unix(),
	}
	go h.update()
	return h
}

func (h *hostParallel) reset() {
	Logf(WARN, "hostParallel::reset. host:%v stream:%v", h.host, h.streams)

	for i := 0; i < len(h.ringCounter); i++ {
		atomic.StoreInt64(&h.ringCounter[i], 0)
	}
	atomic.StoreInt64(&h.count, 0)
	atomic.StoreInt64(&h.expire, 0)
	h.lastDecT = time.Now().Add(-time.Duration(h.p.periods) * time.Second).Unix()
}

func (h *hostParallel) setParallel() {
	Logf(WARN, "hostParallel::setParallel. host:%v stream:%v count:%v", h.host, h.streams, atomic.LoadInt64(&h.count))
	atomic.StoreInt64(&h.expire, time.Now().Add(h.p.duration).UnixNano())
}

func (h *hostParallel) unsetParallel() {
	Logf(WARN, "hostParallel::unsetParallel. host:%v stream:%v count:%v", h.host, h.streams, atomic.LoadInt64(&h.count))
	atomic.StoreInt64(&h.expire, 0)
}

func (h *hostParallel) isParallel() bool {
	return atomic.LoadInt64(&h.expire) != 0
}

func (h *hostParallel) incParallel() {
	idx := int(time.Now().Unix() % int64(h.p.periods+ExtraCachePeriods))
	atomic.AddInt64(&h.ringCounter[idx], 1)
	count := atomic.AddInt64(&h.count, 1)

	streams := atomic.LoadInt64(&h.streams)
	if streams != 0 && float64(count)/float64(streams) >= h.p.rate {
		h.setParallel()
	}
}

func (h *hostParallel) inc() {
	atomic.AddInt64(&h.streams, 1)
}

func (h *hostParallel) dec() {
	atomic.AddInt64(&h.streams, -1)
}

func (h *hostParallel) update() {
	for {
		nowDecT := time.Now().Add(-time.Duration(h.p.periods) * time.Second).Unix()
		if nowDecT-h.lastDecT > ExtraCachePeriods {
			Logf(ERROR, "hostParallel::update dec count delay. host:%v nowDecT:%v lastDecT:%v", h.host, nowDecT, h.lastDecT)
			h.reset()
			continue
		}

		for t := h.lastDecT + 1; t <= nowDecT; t++ {
			idx := t % int64(h.p.periods+ExtraCachePeriods)
			count := atomic.LoadInt64(&h.ringCounter[idx])
			atomic.AddInt64(&h.ringCounter[idx], -count)
			atomic.AddInt64(&h.count, -count)
		}
		h.lastDecT = nowDecT

		expire := atomic.LoadInt64(&h.expire)
		if expire != 0 && time.Now().UnixNano() >= expire {
			h.unsetParallel()
		}

		time.Sleep(time.Duration(UpdateIntervalMs) * time.Millisecond)
	}
}
