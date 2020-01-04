package kcp

import (
	"runtime"
	"sync"
	"time"
)

// SystemTimedSched is the library level fixed-interval timer scheduler,
var SystemTimedSched *TimedSched = NewTimedSched(runtime.NumCPU(), 10*time.Millisecond)

// TimedSched represents the control struct for timed parallel scheduler
type TimedSched struct {
	// prepending tasks
	prependTasks    []func()
	prependLock     sync.Mutex
	chPrependNotify chan struct{}

	// tasks will be distributed through chTask
	chTask   chan func()
	duration time.Duration // interval for scheduler

	dieOnce sync.Once
	die     chan struct{}
}

// NewTimedSched creates a parallel scheduler with parameters parallel and duration
func NewTimedSched(parallel int, duration time.Duration) *TimedSched {
	ts := new(TimedSched)
	ts.chTask = make(chan func())
	ts.die = make(chan struct{})
	ts.duration = duration
	ts.chPrependNotify = make(chan struct{}, 1)

	for i := 0; i < parallel; i++ {
		go ts.sched()
	}
	go ts.prepend()
	return ts
}

func (ts *TimedSched) sched() {
	var tasks []func()
	timer := time.NewTimer(0)
	for {
		select {
		case task := <-ts.chTask:
			tasks = append(tasks, task)
			if len(tasks) == 1 { // trigger timer
				timer.Reset(ts.duration)
			}
		case <-timer.C:
			for k := range tasks {
				tasks[k]()
			}
			tasks = tasks[:0]
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
func (ts *TimedSched) Put(f func()) {
	ts.prependLock.Lock()
	ts.prependTasks = append(ts.prependTasks, f)
	ts.prependLock.Unlock()
	select {
	case ts.chPrependNotify <- struct{}{}:
	default:
	}
}

// Close terminates this scheduler
func (ts *TimedSched) Close() { ts.dieOnce.Do(func() { close(ts.die) }) }
