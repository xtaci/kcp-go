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
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pushConn reads its UDP socket on one goroutine and hands every packet to
// the receiver, the way a packet filter shared by many sessions would.
type pushConn struct {
	*net.UDPConn
	pushed   atomic.Int64
	receiver atomic.Pointer[func([]byte, net.Addr, error)]
	start    sync.Once
}

func (c *pushConn) SetPacketReceiver(fn func([]byte, net.Addr, error)) {
	if fn == nil {
		c.receiver.Store(nil)
		return
	}
	c.receiver.Store(&fn)
	c.start.Do(func() {
		go c.readLoop()
	})
}

func (c *pushConn) readLoop() {
	buf := make([]byte, maxPacketSize)
	for {
		n, addr, err := c.UDPConn.ReadFrom(buf)
		if fn := c.receiver.Load(); fn != nil {
			(*fn)(buf[:n], addr, err)
		}
		if err != nil {
			return
		}
		c.pushed.Add(1)
	}
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

func TestPacketPusherListener(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	pc := &pushConn{UDPConn: udp}
	l, err := ServeConn(nil, 10, 3, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			s, err := l.AcceptKCP()
			if err != nil {
				return
			}
			go handleEcho(s)
		}
	}()

	cli, err := DialWithOptions(udp.LocalAddr().String(), nil, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	cli.SetNoDelay(1, 10, 2, 1)
	if err := echo_tester(cli, 1024, 128); err != nil {
		t.Fatal(err)
	}
	if pc.pushed.Load() == 0 {
		t.Fatal("no packet arrived through the receiver")
	}
}

func TestPacketPusherReleasedOnClose(t *testing.T) {
	cudp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer cudp.Close()
	cpc := &pushConn{UDPConn: cudp}
	sess, err := NewConn3(1, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil, 0, 0, cpc)
	if err != nil {
		t.Fatal(err)
	}
	if cpc.receiver.Load() == nil {
		t.Fatal("client session set no receiver")
	}
	sess.Close()
	if cpc.receiver.Load() != nil {
		t.Fatal("closed client session kept its receiver")
	}

	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	lpc := &pushConn{UDPConn: udp}
	l, err := ServeConn(nil, 0, 0, lpc)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := Dial(udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if _, err := cli.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	s, err := l.AcceptKCP()
	if err != nil {
		t.Fatal(err)
	}

	// Accepted sessions keep receiving after the Listener closes, as they do
	// with a read goroutine, so the receiver stays until the last one goes.
	l.Close()
	if lpc.receiver.Load() == nil {
		t.Fatal("Listener dropped its receiver with a session open")
	}
	s.Close()
	if lpc.receiver.Load() != nil {
		t.Fatal("Listener kept its receiver after its last session closed")
	}
}

func TestPacketPusherListenerClosed(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	pc := &pushConn{UDPConn: udp}
	l, err := ServeConn(nil, 0, 0, pc)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if pc.receiver.Load() != nil {
		t.Fatal("Listener kept the receiver after closing without sessions")
	}

	// A callback already running when Close released the receiver must not
	// register a session that will never get another packet.
	pkt := make([]byte, IKCP_OVERHEAD)
	pkt[0] = 1
	pkt[4] = IKCP_CMD_PUSH
	l.pushedPacket(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil)
	l.sessionLock.RLock()
	n := len(l.sessions)
	l.sessionLock.RUnlock()
	if n != 0 {
		t.Fatalf("closed listener registered %d sessions", n)
	}
}
