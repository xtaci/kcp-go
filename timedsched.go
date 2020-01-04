package kcp

import (
	"container/heap"
	"runtime"
	"sync"
	"time"
)

// SystemTimedSched is the library level timed-scheduler
var SystemTimedSched *TimedSched = NewTimedSched(runtime.NumCPU())

type timedFunc struct {
	execute func()
	ts      time.Time
}

// a heap for sorted time
type timedFuncHeap []timedFunc

func (h timedFuncHeap) Len() int           { return len(h) }
func (h timedFuncHeap) Less(i, j int) bool { return h[i].ts.Before(h[j].ts) }
func (h timedFuncHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *timedFuncHeap) Push(x interface{}) { *h = append(*h, x.(timedFunc)) }

func (h *timedFuncHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// TimedSched represents the control struct for timed parallel scheduler
type TimedSched struct {
	// prepending tasks
	prependTasks    []timedFunc
	prependLock     sync.Mutex
	chPrependNotify chan struct{}

	// tasks will be distributed through chTask
	chTask chan timedFunc

	dieOnce sync.Once
	die     chan struct{}
}

// NewTimedSched creates a parallel scheduler with parameters parallel and duration
func NewTimedSched(parallel int) *TimedSched {
	ts := new(TimedSched)
	ts.chTask = make(chan timedFunc)
	ts.die = make(chan struct{})
	ts.chPrependNotify = make(chan struct{}, 1)

	for i := 0; i < parallel; i++ {
		go ts.sched()
	}
	go ts.prepend()
	return ts
}

func (ts *TimedSched) sched() {
	var tasks timedFuncHeap
	timer := time.NewTimer(0)
	for {
		select {
		case task := <-ts.chTask:
			now := time.Now()
			// delayed! execute immediately
			if now.After(task.ts) {
				task.execute()
			} else {
				heap.Push(&tasks, task)
				// activate timer if timer has hibernated
				// due to no tasks.
				if tasks.Len() == 1 {
					timer.Reset(task.ts.Sub(now))
				}
			}
		case <-timer.C:
			for tasks.Len() > 0 {
				now := time.Now()
				if now.After(tasks[0].ts) {
					heap.Pop(&tasks).(timedFunc).execute()
				} else {
					timer.Reset(tasks[0].ts.Sub(now))
					break
				}
			}
		case <-ts.die:
			return
		}
	}
}

func (ts *TimedSched) prepend() {
	for {
		select {
		case <-ts.chPrependNotify:
			ts.prependLock.Lock()
			tasks := ts.prependTasks
			ts.prependTasks = nil
			ts.prependLock.Unlock()

			for k := range tasks {
				select {
				case ts.chTask <- tasks[k]:
				case <-ts.die:
					return
				}
			}
		case <-ts.die:
			return
		}
	}
}

// Put a function awaiting to be executed
func (ts *TimedSched) Put(f func(), duration time.Duration) {
	ts.prependLock.Lock()
	ts.prependTasks = append(ts.prependTasks, timedFunc{f, time.Now().Add(duration)})
	ts.prependLock.Unlock()
	select {
	case ts.chPrependNotify <- struct{}{}:
	default:
	}
}

// Close terminates this scheduler
func (ts *TimedSched) Close() { ts.dieOnce.Do(func() { close(ts.die) }) }
