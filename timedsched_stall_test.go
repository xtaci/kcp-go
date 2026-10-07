package kcp

import (
	"sync"
	"testing"
	"time"
)

// A shard delayed past schedSlack between popping due tasks and arming its
// timer finds its head already due. It must still wake for it.
func TestTimedSchedArmsWhenHeadPassedWhileArming(t *testing.T) {
	var once sync.Once
	hook := func() { once.Do(func() { time.Sleep(4 * schedSlack) }) }
	testHookBeforeArm.Store(&hook)
	defer testHookBeforeArm.Store(nil)

	ts := NewTimedSched(1)
	defer ts.Close()
	time.Sleep(50 * time.Millisecond) // let the shard park with nothing queued
	ran := make(chan struct{})
	ts.Put(func() { close(ran) }, time.Now().Add(2*schedSlack))
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("task never ran: the shard slept with no timer armed")
	}
}
