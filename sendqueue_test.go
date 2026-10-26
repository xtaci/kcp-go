package kcp

import "testing"

func TestSendQueue(t *testing.T) {
	var sq sendQueue
	if _, ok := sq.pop(); ok {
		t.Fatal("pop from an empty queue")
	}
	for i := 0; i < devBacklog; i++ {
		if !sq.push(sendRequest{buffer: []byte{byte(i)}}) {
			t.Fatalf("push %d refused under the backlog", i)
		}
	}
	if sq.push(sendRequest{}) {
		t.Fatal("push past devBacklog accepted")
	}
	for i := 0; i < devBacklog/2; i++ {
		r, ok := sq.pop()
		if !ok || r.buffer[0] != byte(i) {
			t.Fatalf("pop %d: got %v %v", i, r.buffer, ok)
		}
	}
	// Room again, and order kept across the compaction on the next push.
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
