package kcp

import (
	"net"
	"testing"
	"time"
)

// A write that does not block must not pay for its deadline timer.
func TestWriteWithDeadlineDoesNotAllocate(t *testing.T) {
	l := sinkServer(0)
	defer l.Close()
	cli, err := dialSink(l.Addr().(*net.UDPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	buf := make([]byte, 64)
	write := func() {
		if _, err := cli.Write(buf); err != nil {
			t.Fatal(err)
		}
	}
	cli.SetWriteDeadline(time.Time{})
	without := testing.AllocsPerRun(100, write)
	cli.SetWriteDeadline(time.Now().Add(time.Minute))
	with := testing.AllocsPerRun(100, write)
	if with > without {
		t.Fatalf("Write made %v allocations with a deadline and %v without", with, without)
	}
}
