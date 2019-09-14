package kcp

import (
	"container/heap"
	"hash/fnv"
	"runtime"
	"sync"
	"time"
)

func init() {
	updater.init()
}

var updater updaterMgr

type updaterMgr struct {
	updaters     []*updateHeap
	updaterCount uint32
}

func (mgr *updaterMgr) init() {
	num := runtime.NumCPU()
	mgr.updaters = make([]*updateHeap, num)
	mgr.updaterCount = uint32(num)
	for i := 0; i < num; i++ {
		heap := new(updateHeap)
		heap.init()
		go heap.updateTask()
		mgr.updaters[i] = heap
	}
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func (mgr *updaterMgr) addSession(s *UDPSession) {
	index := hash(s.remote.String()) % mgr.updaterCount
	mgr.updaters[index].addSession(s)
}

func (mgr *updaterMgr) removeSession(s *UDPSession) {
	index := hash(s.remote.String()) % mgr.updaterCount
	mgr.updaters[index].removeSession(s)
}

// entry contains a session update info
type entry struct {
	ts time.Time
	s  *UDPSession
}

// a global heap managed kcp.flush() caller
type updateHeap struct {
	entries  []entry
	mu       sync.Mutex
	chWakeUp chan struct{}
}

func (h *updateHeap) Len() int           { return len(h.entries) }
func (h *updateHeap) Less(i, j int) bool { return h.entries[i].ts.Before(h.entries[j].ts) }
func (h *updateHeap) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.entries[i].s.updaterIdx = i
	h.entries[j].s.updaterIdx = j
}

func (h *updateHeap) Push(x interface{}) {
	h.entries = append(h.entries, x.(entry))
	n := len(h.entries)
	h.entries[n-1].s.updaterIdx = n - 1
}

func (h *updateHeap) Pop() interface{} {
	n := len(h.entries)
	x := h.entries[n-1]
	h.entries[n-1].s.updaterIdx = -1
	h.entries[n-1] = entry{} // manual set nil for GC
	h.entries = h.entries[0 : n-1]
	return x
}

func (h *updateHeap) init() {
	h.chWakeUp = make(chan struct{}, 1)
}

func (h *updateHeap) addSession(s *UDPSession) {
	h.mu.Lock()
	heap.Push(h, entry{time.Now(), s})
	h.mu.Unlock()
	h.wakeup()
}

func (h *updateHeap) removeSession(s *UDPSession) {
	h.mu.Lock()
	if s.updaterIdx != -1 {
		heap.Remove(h, s.updaterIdx)
	}
	h.mu.Unlock()
}

func (h *updateHeap) wakeup() {
	select {
	case h.chWakeUp <- struct{}{}:
	default:
	}
}

func (h *updateHeap) updateTask() {
	timer := time.NewTimer(0)
	for {
		select {
		case <-timer.C:
		case <-h.chWakeUp:
		}

		h.mu.Lock()
		hlen := h.Len()
		for i := 0; i < hlen; i++ {
			entry := &h.entries[0]
			if !time.Now().Before(entry.ts) {
				interval := entry.s.update()
				entry.ts = time.Now().Add(interval)
				heap.Fix(h, 0)
			} else {
				break
			}
		}

		if hlen > 0 {
			timer.Reset(h.entries[0].ts.Sub(time.Now()))
		}
		h.mu.Unlock()
	}
}
