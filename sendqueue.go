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

import "sync"

// sendQueue holds a session's packets waiting for post processing. It
// allocates only while packets are queued, so an idle session costs nothing,
// whereas a channel holding devBacklog requests costs 64 KB per session.
// sendQueueKeep is the buffer a drained queue keeps for the next packets.
const sendQueueKeep = 64

type sendQueue struct {
	mu   sync.Mutex
	q    []sendRequest
	head int
}

// push queues r, or reports false when devBacklog packets are already waiting.
func (sq *sendQueue) push(r sendRequest) bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	if len(sq.q)-sq.head >= devBacklog {
		return false
	}
	if sq.head > 0 && len(sq.q) == cap(sq.q) {
		n := copy(sq.q, sq.q[sq.head:])
		clear(sq.q[n:])
		sq.q, sq.head = sq.q[:n], 0
	}
	sq.q = append(sq.q, r)
	return true
}

// pop takes the oldest packet. A drained queue keeps a buffer only up to
// sendQueueKeep packets, so a burst does not stay allocated.
func (sq *sendQueue) pop() (sendRequest, bool) {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	if sq.head == len(sq.q) {
		return sendRequest{}, false
	}
	r := sq.q[sq.head]
	sq.q[sq.head] = sendRequest{}
	sq.head++
	if sq.head == len(sq.q) {
		sq.q, sq.head = sq.q[:0], 0
		if cap(sq.q) > sendQueueKeep {
			sq.q = nil
		}
	}
	return r, true
}

// len returns the number of queued packets.
func (sq *sendQueue) len() int {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	return len(sq.q) - sq.head
}
