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
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/time/rate"
)

// ppBudget is how many packets a worker takes from one session before it
// moves on, so a busy session cannot starve the others.
const ppBudget = 4 * maxBatchSize

// ppQueue holds the sessions with packets waiting for postProcess. A fixed
// set of workers serves every session, so an idle session holds no goroutine.
// ppRunning ensures that a session appears at most once in this queue or is
// being handled by a worker. head avoids shifting entries after each dequeue.
var ppQueue struct {
	once sync.Once
	mu   sync.Mutex
	cond *sync.Cond
	q    []*UDPSession
	head int
}

// ppWorker owns a batch while a worker transforms queued packets into UDP
// datagrams. Its slices are reused between sessions to reduce allocations.
type ppWorker struct {
	txqueue []ipv4.Message
	bufs    [][]byte
}

// ppWake is the cancellable handle captured by a delayed scheduler task.
// Clearing session releases the session before the task's deadline if the
// held batch is sent or discarded first.
type ppWake struct {
	session atomic.Pointer[UDPSession]
}

// oneBuf appends b to the worker-owned slice and returns a one-element view
// suitable for ipv4.Message.Buffers. recycle clears that view before reuse.
func (w *ppWorker) oneBuf(b []byte) [][]byte {
	w.bufs = append(w.bufs, b)
	return w.bufs[len(w.bufs)-1 : len(w.bufs) : len(w.bufs)]
}

// recycle returns every datagram buffer to the shared pool and clears
// references held by the worker's message and slice arrays.
func (w *ppWorker) recycle() {
	for k := range w.txqueue {
		defaultBufferPool.Put(w.txqueue[k].Buffers[0])
		w.txqueue[k].Buffers = nil
	}
	w.txqueue = w.txqueue[:0]
	clear(w.bufs)
	w.bufs = w.bufs[:0]
}

// ppWorkers reuses batch storage. Usually a worker keeps its ppWorker; when a
// session holds a batch for rate limiting, that worker obtains another one
// until the held batch is sent or discarded.
var ppWorkers = sync.Pool{New: func() any {
	return &ppWorker{
		txqueue: make([]ipv4.Message, 0, maxBatchSize),
		bufs:    make([][]byte, 0, maxBatchSize),
	}
}}

// flushTx sends w immediately or transfers ownership of it to s when the
// session's byte rate limiter reserves a future send time. It returns true
// when the caller must not reuse w: the batch was either held or discarded.
// A worker never waits for limiter tokens, leaving it available to serve other
// sessions.
func (s *UDPSession) flushTx(w *ppWorker, bytesToSend int) bool {
	if s.isClosed() {
		w.recycle()
		ppWorkers.Put(w)
		return true
	}
	if limiter := s.rateLimiter.Load(); limiter != nil {
		now := time.Now()
		// A batch larger than the burst cannot be reserved, so it is sent
		// immediately without rate limiting.
		if r := limiter.ReserveN(now, bytesToSend); r.OK() {
			if d := r.DelayFrom(now); d > 0 {
				if s.setHeldPostProcess(w, bytesToSend, now.Add(d), r) {
					return true
				}
				r.Cancel()
				w.recycle()
				ppWorkers.Put(w)
				return true
			}
		}
	}
	s.sendTx(w, bytesToSend)
	return false
}

// sendTx performs the platform-specific send for a completed batch, emits a
// trace record when enabled, and recycles the batch buffers. ppSendMu keeps
// Close from returning while a send is in progress and rejects sends after
// Close has shut the gate.
func (s *UDPSession) sendTx(w *ppWorker, bytesToSend int) bool {
	s.ppSendMu.Lock()
	defer s.ppSendMu.Unlock()
	if s.ppClosing {
		w.recycle()
		return false
	}
	s.tx(w.txqueue)
	if kcpTrace {
		s.mu.Lock()
		s.kcp.debugLog(IKCP_LOG_OUTPUT, "conv", s.kcp.conv, "datalen", bytesToSend)
		s.mu.Unlock()
	}
	w.recycle()
	return true
}

// kickPostProcess schedules s after its caller enqueues a packet. The atomic
// claim prevents concurrent producers from queueing the same session twice.
func (s *UDPSession) kickPostProcess() {
	if s.claimPostProcess() {
		ppEnqueue(s)
	}
}

// claimPostProcess changes the session from idle to queued-or-running.
func (s *UDPSession) claimPostProcess() bool {
	return s.ppRunning.CompareAndSwap(false, true)
}

// finishPostProcess relinquishes the current worker claim. If the session is
// still open and packets arrived during processing, it claims the session
// again so the caller can enqueue another worker pass. Closed sessions instead
// release any held batch and queued packet buffers.
func (s *UDPSession) finishPostProcess() bool {
	s.ppRunning.Store(false)
	if s.isClosed() {
		s.discardPostProcess()
		return false
	}
	return s.postQueue.len() > 0 && s.claimPostProcess()
}

// setHeldPostProcess transfers ownership of a rate-limited batch and its
// reservation to the session. It refuses the transfer if the session is
// already marked closed.
func (s *UDPSession) setHeldPostProcess(w *ppWorker, bytes int, until time.Time, reservation *rate.Reservation) bool {
	s.ppHeldMu.Lock()
	defer s.ppHeldMu.Unlock()
	if s.isClosed() {
		return false
	}
	s.ppHeld.w, s.ppHeld.bytes, s.ppHeld.until = w, bytes, until
	s.ppHeld.reservation = reservation
	s.ppHeld.wake = &ppWake{}
	s.ppHeld.wake.session.Store(s)
	return true
}

// takeHeldPostProcess atomically removes the session's held batch. It also
// invalidates the associated wake handle so a pending timer no longer retains
// the session.
func (s *UDPSession) takeHeldPostProcess() (*ppWorker, int, *rate.Reservation, *ppWake) {
	s.ppHeldMu.Lock()
	w, bytes, reservation, wake := s.ppHeld.w, s.ppHeld.bytes, s.ppHeld.reservation, s.ppHeld.wake
	s.ppHeld.w, s.ppHeld.bytes, s.ppHeld.until, s.ppHeld.reservation, s.ppHeld.wake = nil, 0, time.Time{}, nil, nil
	s.ppHeldMu.Unlock()
	if wake != nil {
		wake.session.Store(nil)
	}
	return w, bytes, reservation, wake
}

// heldPostProcessUntil snapshots the held batch's deadline and wake handle.
// The held flag is false if Close or a worker has already taken the batch.
func (s *UDPSession) heldPostProcessUntil() (time.Time, *ppWake, bool) {
	s.ppHeldMu.Lock()
	until, wake, held := s.ppHeld.until, s.ppHeld.wake, s.ppHeld.w != nil
	s.ppHeldMu.Unlock()
	return until, wake, held
}

// discardHeldPostProcess cancels an unconsumed rate reservation and recycles
// its held batch. The lock-based take makes cleanup race safely with a worker
// that is claiming the same batch.
func (s *UDPSession) discardHeldPostProcess() {
	if w, _, reservation, _ := s.takeHeldPostProcess(); w != nil {
		if reservation != nil {
			reservation.Cancel()
		}
		w.recycle()
		ppWorkers.Put(w)
	}
}

// discardPostProcess releases held and queued packet buffers after Close.
// Close serializes its final KCP flush and closes die while holding s.mu, so
// the output callback cannot enqueue more packets after this drain completes.
func (s *UDPSession) discardPostProcess() {
	s.discardHeldPostProcess()
	for {
		req, ok := s.postQueue.pop()
		if !ok {
			return
		}
		defaultBufferPool.Put(req.buffer)
	}
}

// ppEnqueue appends a claimed session to the shared worker queue and wakes one
// worker. Queue compaction drops consumed references without shifting on every
// dequeue.
func ppEnqueue(s *UDPSession) {
	ppQueue.once.Do(startPostProcessWorkers)
	ppQueue.mu.Lock()
	if ppQueue.head > len(ppQueue.q)/2 {
		n := copy(ppQueue.q, ppQueue.q[ppQueue.head:])
		clear(ppQueue.q[n:])
		ppQueue.q, ppQueue.head = ppQueue.q[:n], 0
	}
	ppQueue.q = append(ppQueue.q, s)
	ppQueue.mu.Unlock()
	ppQueue.cond.Signal()
}

// startPostProcessWorkers initializes the queue condition variable and starts
// a bounded process-wide pool. It is called exactly once by ppQueue.once.
func startPostProcessWorkers() {
	ppQueue.cond = sync.NewCond(&ppQueue.mu)
	n := runtime.NumCPU()
	if n < 2 {
		n = 2
	}
	if n > 8 {
		n = 8
	}
	for i := 0; i < n; i++ {
		go postProcessWorker()
	}
}

// postProcessWorker repeatedly takes one claimed session from ppQueue. It
// sends a previously held batch when its reservation is due, processes more
// queued packets within the per-session budget, then either requeues the
// session, schedules its held batch, or releases its worker claim.
func postProcessWorker() {
	w := ppWorkers.Get().(*ppWorker)
	for {
		ppQueue.mu.Lock()
		for ppQueue.head == len(ppQueue.q) {
			ppQueue.cond.Wait()
		}
		s := ppQueue.q[ppQueue.head]
		ppQueue.q[ppQueue.head] = nil
		ppQueue.head++
		if ppQueue.head == len(ppQueue.q) {
			ppQueue.q, ppQueue.head = ppQueue.q[:0], 0
		}
		ppQueue.mu.Unlock()

		if s.isClosed() {
			// Close may leave a queue entry behind after draining its buffers.
			// finishPostProcess clears the claim and performs an idempotent drain.
			s.finishPostProcess()
			continue
		}

		// The scheduled wake is at or after the reservation time. Taking the
		// batch invalidates any duplicate or stale wake before it can run.
		if h, bytes, reservation, _ := s.takeHeldPostProcess(); h != nil {
			if !s.sendTx(h, bytes) {
				if reservation != nil {
					reservation.Cancel()
				}
				ppWorkers.Put(h)
				s.finishPostProcess()
				continue
			}
			ppWorkers.Put(h)
		}

		more, held := s.postProcess(w)
		if held {
			// The session owns w now; this worker switches to fresh storage and
			// leaves the session claimed until the scheduled wake re-enqueues it.
			w = ppWorkers.Get().(*ppWorker)
			until, wake, scheduled := s.heldPostProcessUntil()
			if !scheduled {
				s.finishPostProcess()
				continue
			}
			// The scheduler may coalesce tasks by running up to schedSlack early.
			// Add that slack here so the reservation is never used prematurely.
			SystemTimedSched.Put(func() {
				sess := wake.session.Load()
				if sess == nil {
					return
				}
				if sess.isClosed() {
					sess.finishPostProcess()
					return
				}
				ppEnqueue(sess)
			}, until.Add(schedSlack))
			continue
		}
		if more {
			ppEnqueue(s)
			continue
		}
		// Hand back to kickPostProcess, unless a packet arrived meanwhile.
		if s.finishPostProcess() {
			ppEnqueue(s)
		}
	}
}
