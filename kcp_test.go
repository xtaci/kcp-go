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
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xtaci/lossyconn"
)

const repeat = 16

func TestLossyConn1(t *testing.T) {
	t.Log("testing loss rate 10%, rtt 200ms")
	t.Log("testing link with nodelay parameters:1 10 2 1")
	client, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}

	server, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}
	testlink(t, client, server, 1, 10, 2, 1)
}

func TestLossyConn2(t *testing.T) {
	t.Log("testing loss rate 20%, rtt 200ms")
	t.Log("testing link with nodelay parameters:1 10 2 1")
	client, err := lossyconn.NewLossyConn(0.2, 100)
	if err != nil {
		t.Fatal(err)
	}

	server, err := lossyconn.NewLossyConn(0.2, 100)
	if err != nil {
		t.Fatal(err)
	}
	testlink(t, client, server, 1, 10, 2, 1)
}

func TestLossyConn3(t *testing.T) {
	t.Log("testing loss rate 30%, rtt 200ms")
	t.Log("testing link with nodelay parameters:1 10 2 1")
	client, err := lossyconn.NewLossyConn(0.3, 100)
	if err != nil {
		t.Fatal(err)
	}

	server, err := lossyconn.NewLossyConn(0.3, 100)
	if err != nil {
		t.Fatal(err)
	}
	testlink(t, client, server, 1, 10, 2, 1)
}

func TestLossyConn4(t *testing.T) {
	t.Log("testing loss rate 10%, rtt 200ms")
	t.Log("testing link with nodelay parameters:1 10 2 0")
	client, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}

	server, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}
	testlink(t, client, server, 1, 10, 2, 0)
}

func testlink(t *testing.T, client *lossyconn.LossyConn, server *lossyconn.LossyConn, nodelay, interval, resend, nc int) {
	t.Log("testing with nodelay parameters:", nodelay, interval, resend, nc)
	sess, _ := NewConn2(server.LocalAddr(), nil, 0, 0, client)
	listener, _ := ServeConn(nil, 0, 0, server)
	echoServer := func(l *Listener) {
		for {
			conn, err := l.AcceptKCP()
			if err != nil {
				return
			}
			go func() {
				conn.SetNoDelay(nodelay, interval, resend, nc)
				buf := make([]byte, 65536)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					conn.Write(buf[:n])
				}
			}()
		}
	}

	echoTester := func(s *UDPSession, raddr net.Addr) {
		s.SetNoDelay(nodelay, interval, resend, nc)
		buf := make([]byte, 64)
		var rtt time.Duration
		for i := 0; i < repeat; i++ {
			start := time.Now()
			s.Write(buf)
			io.ReadFull(s, buf)
			rtt += time.Since(start)
		}

		t.Log("client:", client)
		t.Log("server:", server)
		t.Log("avg rtt:", rtt/repeat)
		t.Logf("total time: %v for %v round trip:", rtt, repeat)
	}

	go echoServer(listener)
	echoTester(sess, server.LocalAddr())
}

func BenchmarkFlush(b *testing.B) {
	kcp := NewKCP(1, func(buf []byte, size int) {})
	kcp.snd_buf = make([]segment, 1024)
	for k := range kcp.snd_buf {
		kcp.snd_buf[k].xmit = 1
		kcp.snd_buf[k].resendts = currentMs() + 10000
	}
	b.ResetTimer()
	b.ReportAllocs()
	var mu sync.Mutex
	for i := 0; i < b.N; i++ {
		mu.Lock()
		kcp.flush(false)
		mu.Unlock()
	}
}
