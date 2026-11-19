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
	running    atomic.Bool   // true if a worker is processing this session
	activeCall *libsync.Quit // active postProcess calls
	queue      sendQueue     // packets waiting for postProcess
}

// senderQueue holds the sessions with packets waiting for postProcess. A fixed
// set of workers serves every session, so an idle session holds no goroutine.
// sender.running ensures that a session appears at most once in this queue or is
// being handled by a worker. head avoids shifting entries after each dequeue.
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

// append appends b to the worker-owned slice and returns a one-element view
// suitable for ipv4.Message.Buffers. recycle clears that view before reuse.
func (w *txWorker) append(b []byte) [][]byte {
	w.bufs = append(w.bufs, b)
	return w.bufs[len(w.bufs)-1 : len(w.bufs) : len(w.bufs)]
}

// recycle returns every datagram buffer to the shared pool and clears
// references held by the worker's message and slice arrays.
func (w *txWorker) recycle() {
	for k := range w.txqueue {
		defaultBufferPool.Put(w.txqueue[k].Buffers[0])
		w.txqueue[k].Buffers = nil
	}

	w.txqueue = w.txqueue[:0]
	clear(w.bufs)
	w.bufs = w.bufs[:0]
}

// txWorkers reuses batch storage across post-processing workers.
var txWorkers = sync.Pool{New: func() any {
	return &txWorker{
		txqueue: make([]ipv4.Message, 0, maxBatchSize),
		bufs:    make([][]byte, 0, maxBatchSize),
	}
}}

// sendTx sends a batch from an active postProcess call and recycles its buffers.
func (s *UDPSession) sendTx(w *txWorker, bytesToSend int) {
	if s.isClosed() {
		w.recycle()
		return
	}

	s.tx(w.txqueue)
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
	s.sender.running.Store(false)
	if s.isClosed() {
		return false
	}
	return s.sender.queue.len() > 0 && s.claimSender()
}

// discardSender returns queued packet buffers after producers and workers stop.
func (s *UDPSession) discardSender() {
	for {
		req, ok := s.sender.queue.pop()
		if !ok {
			return
		}
		defaultBufferPool.Put(req.buffer)
	}
}

// senderEnqueue asenderends a claimed session to the shared worker queue and wakes one
// worker. Queue compaction drops consumed references without shifting on every
// dequeue.
func senderEnqueue(s *UDPSession) {
	senderQueue.once.Do(startSenderWorkers)

	defer senderQueue.cond.Signal()
	senderQueue.mutex.Lock()
	defer senderQueue.mutex.Unlock()

	if senderQueue.head > len(senderQueue.queue)/2 {
		n := copy(senderQueue.queue, senderQueue.queue[senderQueue.head:])
		clear(senderQueue.queue[n:])
		senderQueue.queue, senderQueue.head = senderQueue.queue[:n], 0
	}
	senderQueue.queue = append(senderQueue.queue, s)
}

// startSenderWorkers initializes the queue condition variable and starts
// a bounded process-wide pool. It is called exactly once by ppQueue.once.
func startSenderWorkers() {
	senderQueue.cond = sync.NewCond(&senderQueue.mutex)

	n := min(max(runtime.NumCPU(), 2), 8)
	for range n {
		go sendWorker()
	}
}

// sendWorker repeatedly takes one claimed session from senderQueue,
// processes packets within the per-session budget, and requeues the session
// when more packets remain.
func sendWorker() {
	w := txWorkers.Get().(*txWorker)
	for {
		senderQueue.mutex.Lock()
		for senderQueue.head == len(senderQueue.queue) {
			senderQueue.cond.Wait()
		}
		s := senderQueue.queue[senderQueue.head]
		senderQueue.queue[senderQueue.head] = nil
		senderQueue.head++
		if senderQueue.head == len(senderQueue.queue) {
			senderQueue.queue, senderQueue.head = senderQueue.queue[:0], 0
		}
		senderQueue.mutex.Unlock()

		if s.isClosed() || !s.sender.activeCall.Add(1) {
			s.finishSender()
			continue
		}

		more := s.postProcess(w)
		s.sender.activeCall.Done()
		if more && !s.isClosed() {
			senderEnqueue(s)
			continue
		}
		// Hand back to kickSender, unless a packet arrived meanwhile.
		if s.finishSender() {
			senderEnqueue(s)
		}
	}
}
