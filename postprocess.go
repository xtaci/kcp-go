package kcp

import (
	"context"
	"runtime"
	"sync"

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

func (s *UDPSession) flushTx(w *ppWorker, bytesToSend int) {
	if limiter, ok := s.rateLimiter.Load().(*rate.Limiter); ok {
		// WaitN only returns error if the limiter is misconfigured
		// or context is cancelled. In either case, we continue sending.
		_ = limiter.WaitN(context.Background(), bytesToSend)
	}
	s.tx(w.txqueue)
	if kcpTrace {
		s.kcp.debugLog(IKCP_LOG_OUTPUT, "conv", s.kcp.conv, "datalen", bytesToSend)
	}
	w.recycle()
}

// kickPostProcess queues s for a worker unless it is already queued or being
// served. Callers queue their packet first.
func (s *UDPSession) kickPostProcess() {
	if s.ppRunning.CompareAndSwap(false, true) {
		ppEnqueue(s)
	}
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
	w := &ppWorker{
		txqueue: make([]ipv4.Message, 0, maxBatchSize),
		bufs:    make([][]byte, 0, maxBatchSize),
	}
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

		if s.postProcess(w) {
			ppEnqueue(s)
			continue
		}
		// Hand back to kickPostProcess, unless a packet arrived meanwhile.
		s.ppRunning.Store(false)
		if len(s.chPostProcessing) > 0 && s.ppRunning.CompareAndSwap(false, true) {
			ppEnqueue(s)
		}
	}
}
