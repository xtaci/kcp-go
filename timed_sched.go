package kcp

import (
	"container/heap"
	"runtime"
	"sync"
	"time"
)

const (
	TS_NORMAL = iota
	TS_ONCE
	TS_EXCLUSIVE
)

// SystemTimedSched is the library level timed-scheduler
var SystemTimedSched *TimedSchedPool = NewTimedSchedPool(runtime.NumCPU())

type timedFunc struct {
	fnvKey   uint32
	mode     int
	execute  func()
	delayMs  uint32
	expireMs uint32
	index    int
}

// a heap for sorted timed function
type timedFuncHeap []*timedFunc

func (h timedFuncHeap) Len() int {
	return len(h)
}

func (h timedFuncHeap) Less(i, j int) bool {
	return h[i].expireMs < h[j].expireMs
}

func (h timedFuncHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *timedFuncHeap) Push(x interface{}) {
	n := len(*h)
	task := x.(*timedFunc)
	task.index = n
	*h = append(*h, task)
}

func (h *timedFuncHeap) Pop() interface{} {
	old := *h
	n := len(old)
	task := old[n-1]
	old[n-1].execute = nil // avoid memory leak
	old[n-1].index = -1
	old[n-1] = nil
	*h = old[0 : n-1]
	return task
}

// TimedSched represents the control struct for timed parallel scheduler
type TimedSched struct {
	// prepending tasks
	prependTasks    [2][]timedFunc
	prependIdx      int
	prependLock     sync.Mutex
	chPrependNotify chan struct{}

	cleanKeys     []uint32
	cleanLock     sync.Mutex
	chCleanNotify chan struct{}

	// tasks will be distributed through chTask
	chTask   chan timedFunc
	keyTimer map[uint32]*timedFunc
	timer    *time.Timer

	taskHeap timedFuncHeap
}

// NewTimedSched creates a parallel-scheduler with given parallelization
func NewTimedSched() *TimedSched {
	ts := new(TimedSched)
	ts.chPrependNotify = make(chan struct{}, 1)
	ts.chCleanNotify = make(chan struct{}, 1)
	ts.chTask = make(chan timedFunc)
	ts.keyTimer = make(map[uint32]*timedFunc)
	ts.timer = time.NewTimer(0)

	go ts.sched()
	return ts
}

func (ts *TimedSched) newTasks(tasks []timedFunc) (drained bool) {
	var top *timedFunc
	current := currentMs()
	for k := range tasks {
		task := tasks[k]
		tasks[k].execute = nil
		if task.mode == TS_ONCE {
			if _, ok := ts.keyTimer[task.fnvKey]; ok {
				continue
			}
		} else if task.mode == TS_EXCLUSIVE {
			if item, ok := ts.keyTimer[task.fnvKey]; ok {
				heap.Remove(&ts.taskHeap, item.index)
			}
		}
		if task.delayMs == 0 || current >= task.expireMs {
			task.execute()
			continue
		}
		heap.Push(&ts.taskHeap, &task)
		if task.mode == TS_ONCE || task.mode == TS_EXCLUSIVE {
			ts.keyTimer[task.fnvKey] = &task
		}
		if task.index == 0 {
			top = &task
		}
	}
	if top != nil {
		stopped := ts.timer.Stop()
		if !stopped && !drained {
			<-ts.timer.C
		}
		delta := currentMs() - top.expireMs
		if delta < 0 {
			delta = 0
		}
		ts.timer.Reset(time.Duration(delta) * time.Millisecond)
		return true
	}
	return false
}

func (ts *TimedSched) advanceTasks() bool {
	current := currentMs()
	for ts.taskHeap.Len() > 0 {
		task := ts.taskHeap[0]
		if current >= task.expireMs {
			heap.Pop(&ts.taskHeap).(*timedFunc).execute()
			if task.mode == TS_ONCE || task.mode == TS_EXCLUSIVE {
				delete(ts.keyTimer, task.fnvKey)
			}
		}
		current = currentMs()
		if current < task.expireMs {
			ts.timer.Reset(time.Duration(task.expireMs-current) * time.Millisecond)
			return true
		}
	}
	return false
}

func (ts *TimedSched) cleanTasks(cleanKeys []uint32) {
	for k := range cleanKeys {
		timer, ok := ts.keyTimer[cleanKeys[k]]
		if ok {
			heap.Remove(&ts.taskHeap, timer.index)
			delete(ts.keyTimer, cleanKeys[k])
		}
	}
}

func (ts *TimedSched) sched() {
	drained := false

	for {
		select {
		case <-ts.chPrependNotify:
			ts.prependLock.Lock()
			tasks := ts.prependTasks[ts.prependIdx]
			ts.prependTasks[ts.prependIdx] = ts.prependTasks[ts.prependIdx][:0]
			ts.prependIdx = (ts.prependIdx + 1) % 2
			ts.prependLock.Unlock()
			ts.newTasks(tasks)
		case <-ts.timer.C:
			drained = true
			ts.advanceTasks()
		case <-ts.chCleanNotify:
			ts.cleanLock.Lock()
			ts.cleanTasks(ts.cleanKeys)
			ts.cleanKeys = ts.cleanKeys[:0]
			ts.cleanLock.Unlock()
		}
	}
}

// Put a function 'f' awaiting to be executed at 'deadline'
func (ts *TimedSched) Put(fnvkey uint32, mode int, f func(), delayMs uint32) {
	ts.prependLock.Lock()
	ts.prependTasks[ts.prependIdx] = append(ts.prependTasks[ts.prependIdx], timedFunc{
		fnvKey:   fnvkey,
		mode:     mode,
		execute:  f,
		delayMs:  delayMs,
		expireMs: currentMs() + delayMs,
	})
	ts.prependLock.Unlock()

	select {
	case ts.chPrependNotify <- struct{}{}:
	default:
	}
}

func (ts *TimedSched) Clean(fnvKey uint32) {
	ts.cleanLock.Lock()
	ts.cleanKeys = append(ts.cleanKeys, fnvKey)
	ts.cleanLock.Unlock()

	select {
	case ts.chCleanNotify <- struct{}{}:
	default:
	}
}

type TimedSchedPool struct {
	pool []*TimedSched
}

// NewTimedSched creates a parallel-scheduler with given parallelization
func NewTimedSchedPool(parallel int) *TimedSchedPool {
	pool := make([]*TimedSched, parallel)
	for i := 0; i < parallel; i++ {
		pool[i] = NewTimedSched()
	}
	return &TimedSchedPool{pool: pool}
}

func (p *TimedSchedPool) Pick(fnvKey uint32) *TimedSched {
	return p.pool[fnvKey%uint32(len(p.pool))]
}
