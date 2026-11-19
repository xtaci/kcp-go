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

	libsync "github.com/xtaci/kcp-go/v5/internal/lib/sync"
	"golang.org/x/net/ipv4"
)

// senderBudget is how many packets a worker takes from one session before it
// moves on, so a busy session cannot starve the others.
const senderBudget = 4 * maxBatchSize

type sender struct {
	running          atomic.Bool   // scheduling claim: queued or being processed
	postProcessCalls *libsync.Quit // tracks postProcess calls so Close can wait for them
	queue            sendQueue     // packets waiting for postProcess
}

// senderQueue holds the sessions with packets waiting for postProcess. A fixed
// set of workers serves every session, so an idle session holds no goroutine.
// sender.running holds one scheduling claim per session, covering both queued
// and processing work. head avoids shifting entries after each dequeue.
var senderQueue struct {
	once  sync.Once
	mutex sync.Mutex
	cond  *sync.Cond
	queue []*UDPSession
	head  int
}

// txWorker owns a batch while a worker transforms queued packets into UDP
// datagrams. Its slices are reused between sessions to reduce allocations.
type txWorker struct {
	txqueue []ipv4.Message
	bufs    [][]byte
}

// append stores b in the worker-owned slice and returns a one-element view
// suitable for ipv4.Message.Buffers. The capacity limit keeps an append to
// that view from overwriting the next buffer in w.bufs.
func (w *txWorker) append(b []byte) [][]byte {
	w.bufs = append(w.bufs, b)
	return w.bufs[len(w.bufs)-1 : len(w.bufs) : len(w.bufs)]
}

// recycle returns every datagram buffer to the shared pool and clears the
// buffer references held by the worker's message and slice arrays.
func (w *txWorker) recycle() {
	for k := range w.txqueue {
		defaultBufferPool.Put(w.txqueue[k].Buffers[0])
		w.txqueue[k].Buffers = nil
	}

	// Reset both slices only after returning the buffers held by the messages.
	w.txqueue = w.txqueue[:0]
	clear(w.bufs)
	w.bufs = w.bufs[:0]
}

// txWorkers supplies reusable batch storage for post-processing workers.
var txWorkers = sync.Pool{New: func() any {
	return &txWorker{
		txqueue: make([]ipv4.Message, 0, maxBatchSize),
		bufs:    make([][]byte, 0, maxBatchSize),
	}
}}

// sendTx sends a batch from an active postProcess call and recycles its buffers.
func (s *UDPSession) sendTx(w *txWorker, bytesToSend int) {
	// If shutdown is already visible, discard the batch instead of sending.
	// A send can still start if shutdown races with this check; Close waits
	// for the active postProcess call, including that send, to finish.
	if s.isClosed() {
		w.recycle()
		return
	}

	s.tx(w.txqueue)
	// KCP state is shared with the session's other goroutines.
	if kcpTrace {
		s.mutex.Lock()
		s.kcp.debugLog(IKCP_LOG_OUTPUT, "conv", s.kcp.conv, "datalen", bytesToSend)
		s.mutex.Unlock()
	}
	w.recycle()
}

// kickSender schedules s after its caller enqueues a packet. The atomic
// claim prevents concurrent producers from queueing the same session twice.
func (s *UDPSession) kickSender() {
	// The producer has already placed its packet in the session queue.
	// A worker holding the claim will either process it or arrange another
	// pass; otherwise this producer claims and schedules the session.
	if !s.isClosed() && s.claimSender() {
		senderEnqueue(s)
	}
}

// claimSender changes the session from idle to queued-or-running.
func (s *UDPSession) claimSender() bool {
	return s.sender.running.CompareAndSwap(false, true)
}

// finishSender relinquishes the current worker claim. If the session is
// still open and packets arrived during processing, it claims the session
// again so the caller can enqueue another worker pass. Close discards the queue
// after waiting for active workers.
func (s *UDPSession) finishSender() bool {
	// Release before checking the queue so a concurrent producer can claim
	// the session. If a producer queued work while the old claim was held,
	// this worker sees it and attempts to reclaim. If the producer claims
	// first, the CAS fails and that producer is responsible for scheduling.
	s.sender.running.Store(false)
	if s.isClosed() {
		return false
	}
	return s.sender.queue.len() > 0 && s.claimSender()
}

// discardSender returns queued packet buffers after producers and workers stop.
func (s *UDPSession) discardSender() {
	// Close calls this only after synchronizing with producers and waiting
	// for active postProcess calls, so no worker can consume these buffers.
	for {
		req, ok := s.sender.queue.pop()
		if !ok {
			return
		}
		defaultBufferPool.Put(req.buffer)
	}
}

// senderEnqueue appends a claimed session to the shared worker queue and wakes
// one worker. Queue compaction drops consumed references without shifting on
// every dequeue.
func senderEnqueue(s *UDPSession) {
	senderQueue.once.Do(startSenderWorkers)

	// Signal after unlocking; the queue predicate itself is protected by
	// the mutex, so a worker cannot miss the newly enqueued session.
	defer senderQueue.cond.Signal()
	senderQueue.mutex.Lock()
	defer senderQueue.mutex.Unlock()

	// Compact only after more than half the entries have been consumed.
	if senderQueue.head > len(senderQueue.queue)/2 {
		n := copy(senderQueue.queue, senderQueue.queue[senderQueue.head:])
		clear(senderQueue.queue[n:])
		senderQueue.queue, senderQueue.head = senderQueue.queue[:n], 0
	}
	senderQueue.queue = append(senderQueue.queue, s)
}

// startSenderWorkers initializes the queue condition variable and starts
// a bounded process-wide pool. It is called exactly once by senderQueue.once.
func startSenderWorkers() {
	senderQueue.cond = sync.NewCond(&senderQueue.mutex)

	// Bound the number of long-lived goroutines, even on large machines.
	n := min(max(runtime.NumCPU(), 2), 8)
	for range n {
		go sendWorker()
	}
}

// sendWorker repeatedly takes one claimed session from senderQueue,
// processes packets within the per-session budget, and requeues the session
// when more packets remain.
func sendWorker() {
	// Each worker retains its batch storage and reuses it across sessions.
	w := txWorkers.Get().(*txWorker)
	for {
		senderQueue.mutex.Lock()
		// Wait in a loop because another worker may consume the available
		// session before this worker reacquires the mutex.
		for senderQueue.head == len(senderQueue.queue) {
			senderQueue.cond.Wait()
		}
		s := senderQueue.queue[senderQueue.head]
		// Release the queue's reference immediately; reset the indices once
		// the queue drains so its backing slice can be reused.
		senderQueue.queue[senderQueue.head] = nil
		senderQueue.head++
		if senderQueue.head == len(senderQueue.queue) {
			senderQueue.queue, senderQueue.head = senderQueue.queue[:0], 0
		}
		senderQueue.mutex.Unlock()

		// Admission through postProcessCalls closes the race with Close: either
		// this pass is registered and Close waits for it, or Add rejects it.
		if s.isClosed() || !s.sender.postProcessCalls.Add(1) {
			s.finishSender()
			continue
		}

		more := s.postProcess(w)
		s.sender.postProcessCalls.Done()
		if more && !s.isClosed() {
			// Keep the claim while moving the session to the queue's tail,
			// giving other sessions a turn after this worker's budget.
			senderEnqueue(s)
			continue
		}
		// Release the claim and check for work that arrived during handoff.
		if s.finishSender() {
			senderEnqueue(s)
		}
	}
}
