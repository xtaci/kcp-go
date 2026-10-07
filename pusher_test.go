package kcp

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// pushConn reads its UDP socket on one goroutine and hands every packet to
// the receiver, the way a packet filter shared by many sessions would.
type pushConn struct {
	*net.UDPConn
	pushed atomic.Int64
}

func (c *pushConn) SetPacketReceiver(fn func([]byte, net.Addr, error)) {
	go func() {
		buf := make([]byte, mtuLimit)
		for {
			n, addr, err := c.UDPConn.ReadFrom(buf)
			fn(buf[:n], addr, err)
			if err != nil {
				return
			}
			c.pushed.Add(1)
		}
	}()
}

func TestPacketPusherEcho(t *testing.T) {
	port := nextPort()
	l := echoServer(port, nil)
	defer l.Close()

	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	pc := &pushConn{UDPConn: udp}
	raddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	sess, err := NewConn3(1, raddr, nil, 10, 3, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	sess.SetNoDelay(1, 10, 2, 1)

	if err := echo_tester(sess, 1024, 128); err != nil {
		t.Fatal(err)
	}
	if pc.pushed.Load() == 0 {
		t.Fatal("no packet arrived through the receiver")
	}

	// A read error from the conn ends the session's reads.
	udp.Close()
	sess.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := sess.Read(make([]byte, 16)); err == nil {
		t.Fatal("read succeeded after the conn failed")
	}
}
