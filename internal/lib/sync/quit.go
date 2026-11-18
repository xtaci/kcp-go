package sync

import (
	"sync"
)

// Quit coordinates graceful shutdown and task tracking across goroutines.
// It combines a cancellation signal channel with a sync.WaitGroup to ensure
// that no new tasks can be added after shutdown begins, and that callers
// can wait for all active tasks to complete.
type Quit struct {
	done  chan struct{}
	mutex sync.RWMutex
	tasks sync.WaitGroup
}

// NewQuit allocates and returns a new Quit coordinator in an active state.
func NewQuit() *Quit {
	return &Quit{
		done: make(chan struct{}),
	}
}

// Signal returns a channel that is closed when shutdown has been initiated
// via Wait or Close. Goroutines can select on this channel to listen for exit signals.
func (q *Quit) Signal() <-chan struct{} {
	return q.done
}

// Add increments the internal task counter by delta if the coordinator is still active.
// It returns true if the task was successfully added, or false if shutdown has already begun.
func (q *Quit) Add(delta int) bool {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	select {
	case <-q.done:
		return false
	default:
		q.tasks.Add(delta)
		return true
	}
}

// Done decrements the internal task counter. It should be called by worker
// goroutines when a task finishes, matching each successful Add.
func (q *Quit) Done() {
	q.tasks.Done()
}

// Wait initiates shutdown (if not already closed) by closing the signal channel,
// then blocks until all active tasks tracked by Add/Done have completed.
// It returns true if it successfully initiated the shutdown, or false if
// shutdown was already initiated previously.
func (q *Quit) Wait() bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	select {
	case <-q.done:
		return false
	default:
		close(q.done)
		q.tasks.Wait()
		return true
	}
}

// Close initiates shutdown by closing the signal channel without waiting
// for active tasks to finish.
// It returns true if it successfully initiated the shutdown, or false if
// shutdown was already initiated previously.
func (q *Quit) Close() bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	select {
	case <-q.done:
		return false
	default:
		close(q.done)
		return true
	}
}
