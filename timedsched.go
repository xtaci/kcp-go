package kcp

import (
	"container/heap"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SystemTimedSched is the library level timed-scheduler
var SystemTimedSched = NewTimedSched(runtime.NumCPU())

type timedFunc struct {
	execute func() int
	ts      time.Time
}

// a heap for sorted timed function
type timedFuncHeap []timedFunc

func (h timedFuncHeap) Len() int            { return len(h) }
func (h timedFuncHeap) Less(i, j int) bool  { return h[i].ts.Before(h[j].ts) }
func (h timedFuncHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *timedFuncHeap) Push(x interface{}) { *h = append(*h, x.(timedFunc)) }
func (h *timedFuncHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1].execute = nil // avoid memory leak
	*h = old[0 : n-1]
	return x
}

// TimedSched represents the control struct for timed parallel scheduler
type TimedSched struct {
	// tasks will be distributed through chTask
	chTasks []chan timedFunc
	chTaskCount []int64

	dieOnce sync.Once
	die     chan struct{}
}

// NewTimedSched creates a parallel-scheduler with given parallelization
func NewTimedSched(parallel int) *TimedSched {
	ts := new(TimedSched)
	ts.chTasks = make([]chan timedFunc, parallel)
	ts.chTaskCount = make([]int64, parallel)
	ts.die = make(chan struct{})

	for i := 0; i < parallel; i++ {
		ts.chTasks[i] = make(chan timedFunc, 10240)
		go ts.sched(i)
	}

	return ts
}

func (ts *TimedSched) sched(index int) {
	var tasks timedFuncHeap
	timer := time.NewTimer(time.Second*1)
	for {
		select {
		case task := <-ts.chTasks[index]:
			heap.Push(&tasks, task)

			if v := tasks[0].ts.Sub(time.Now()); v > 0 {
				timer.Reset(v)
			} else {
				timer.Reset(0)
			}
		case now := <-timer.C:
			for tasks.Len() > 0 {
				if !now.After(tasks[0].ts) {
					break
				}

				task := heap.Pop(&tasks).(timedFunc)
				if v := task.execute(); v > 0 {
					task.ts = now.Add(time.Duration(v)*time.Millisecond)
					heap.Push(&tasks, task)
				} else {
					atomic.AddInt64(&ts.chTaskCount[index], -1)
				}
			}

			if tasks.Len() > 0 {
				timer.Reset(tasks[0].ts.Sub(now))
			} else {
				timer.Reset(time.Second*1)
			}
		case <-ts.die:
			return
		}
	}
}

// Put a function 'f' awaiting to be executed at 'deadline'
func (ts *TimedSched) Put(f func() int, deadline time.Time) {
	index := 0
	minc := ts.chTaskCount[0]
	for k,v := range ts.chTaskCount {
		if v < minc {
			index = k
			minc = v
		}
	}

	atomic.AddInt64(&ts.chTaskCount[index], 1)
	ts.chTasks[index] <- timedFunc{f, deadline}
}

// Close terminates this scheduler
func (ts *TimedSched) Close() { ts.dieOnce.Do(func() { close(ts.die) }) }
