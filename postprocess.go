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
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/time/rate"
)

// ppBudget is how many packets a worker takes from one session before it
// moves on, so a busy session cannot starve the others.
const ppBudget = 4 * maxBatchSize

// ppQueue holds the sessions with packets waiting for postProcess. A fixed
// set of workers serves every session, so an idle session holds no goroutine.
var ppQueue struct {
	once sync.Once
	mu   sync.Mutex
	cond *sync.Cond
	q    []*UDPSession
	head int
}

// ppWorker holds the TX batch of one worker, reused for every session it serves.
type ppWorker struct {
	txqueue []ipv4.Message
	bufs    [][]byte
}

// oneBuf returns a one-element Buffers slice for b, backed by w.bufs.
func (w *ppWorker) oneBuf(b []byte) [][]byte {
	w.bufs = append(w.bufs, b)
	return w.bufs[len(w.bufs)-1 : len(w.bufs) : len(w.bufs)]
}

func (w *ppWorker) recycle() {
	for k := range w.txqueue {
		defaultBufferPool.Put(w.txqueue[k].Buffers[0])
		w.txqueue[k].Buffers = nil
	}
	w.txqueue = w.txqueue[:0]
	clear(w.bufs)
	w.bufs = w.bufs[:0]
}

// ppWorkers recycles batches handed to a rate limited session and back.
var ppWorkers = sync.Pool{New: func() any {
	return &ppWorker{
		txqueue: make([]ipv4.Message, 0, maxBatchSize),
		bufs:    make([][]byte, 0, maxBatchSize),
	}
}}

// flushTx sends the batch in w. When the rate limiter needs time first, s
// keeps the batch and flushTx reports true, so that no worker sleeps while
// other sessions wait for it.
func (s *UDPSession) flushTx(w *ppWorker, bytesToSend int) bool {
	if limiter, ok := s.rateLimiter.Load().(*rate.Limiter); ok {
		now := time.Now()
		// A batch larger than the burst cannot be reserved, so it is sent
		// immediately without rate limiting.
		if r := limiter.ReserveN(now, bytesToSend); r.OK() {
			if d := r.DelayFrom(now); d > 0 {
				s.ppHeld.w, s.ppHeld.bytes, s.ppHeld.until = w, bytesToSend, now.Add(d)
				return true
			}
		}
	}
	s.sendTx(w, bytesToSend)
	return false
}

func (s *UDPSession) sendTx(w *ppWorker, bytesToSend int) {
	s.tx(w.txqueue)
	if kcpTrace {
		s.mu.Lock()
		s.kcp.debugLog(IKCP_LOG_OUTPUT, "conv", s.kcp.conv, "datalen", bytesToSend)
		s.mu.Unlock()
	}
	w.recycle()
}

// kickPostProcess queues s for a worker unless it is already queued or being
// served. Callers queue their packet first.
func (s *UDPSession) kickPostProcess() {
	if s.claimPostProcess() {
		ppEnqueue(s)
	}
}

func (s *UDPSession) claimPostProcess() bool {
	return s.ppRunning.CompareAndSwap(false, true)
}

func (s *UDPSession) finishPostProcess() bool {
	s.ppRunning.Store(false)
	return s.postQueue.len() > 0 && s.claimPostProcess()
}

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

		// The limiter allowed a held batch when it was held, so send it now.
		if h := s.ppHeld.w; h != nil {
			s.ppHeld.w = nil
			s.sendTx(h, s.ppHeld.bytes)
			ppWorkers.Put(h)
		}

		more := s.postProcess(w)
		if s.ppHeld.w == w {
			// s keeps the batch, and stays queued, until the limiter allows it.
			w = ppWorkers.Get().(*ppWorker)
			SystemTimedSched.Put(func() { ppEnqueue(s) }, s.ppHeld.until)
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
