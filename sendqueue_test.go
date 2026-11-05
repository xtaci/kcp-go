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

import "testing"

func TestSendQueue(t *testing.T) {
	var sq sendQueue
	if _, ok := sq.pop(); ok {
		t.Fatal("pop from an empty queue")
	}
	for i := range devBacklog {
		if !sq.push(sendRequest{buffer: []byte{byte(i)}}) {
			t.Fatalf("push %d refused under the backlog", i)
		}
	}
	if sq.push(sendRequest{}) {
		t.Fatal("push past devBacklog accepted")
	}
	for i := range devBacklog / 2 {
		r, ok := sq.pop()
		if !ok || r.buffer[0] != byte(i) {
			t.Fatalf("pop %d: got %v %v", i, r.buffer, ok)
		}
	}
	// There is room again, and order is preserved across compaction on the next push.
	if !sq.push(sendRequest{buffer: []byte{0xff}}) {
		t.Fatal("push refused after pops")
	}
	for i := devBacklog / 2; i < devBacklog; i++ {
		if r, _ := sq.pop(); r.buffer[0] != byte(i) {
			t.Fatalf("pop %d after compaction: got %v", i, r.buffer)
		}
	}
	if r, _ := sq.pop(); r.buffer[0] != 0xff {
		t.Fatalf("last pop: got %v", r.buffer)
	}
	if sq.len() != 0 || sq.q != nil {
		t.Fatalf("drained queue keeps len %d cap %d", sq.len(), cap(sq.q))
	}
}
