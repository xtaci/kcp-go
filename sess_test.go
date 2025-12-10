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
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/pbkdf2"
)

var (
	baseport = uint32(10000)
	key      = []byte("testkey")
	pass     = pbkdf2.Key(key, []byte("testsalt"), 4096, 32, sha1.New)
)

func init() {
	go func() {
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()

	log.Println("beginning tests, encryption:salsa20, fec:10/3")
}

func nextPort() int {
	port := int(atomic.AddUint32(&baseport, 1))
	return port % 65536
}

func dialEcho(port int, block BlockCrypt) (*UDPSession, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
	if err != nil {
		panic(err)
		return nil, err
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetMtu(1600)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(true)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	sess.SetRateLimit(200 * 1024 * 1024)
	sess.SetLogger(IKCP_LOG_ALL, newLoggerWithMilliseconds().Info)
	return sess, nil
}

func dialSink(port int) (*UDPSession, error) {
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
	if err != nil {
		panic(err)
		return nil, err
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, nil
}

func dialTinyBufferEcho(port int) (*UDPSession, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
	if err != nil {
		panic(err)
		return nil, err
	}
	return sess, nil
}

// ////////////////////////
func listenEcho(port int, block BlockCrypt) (net.Listener, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 1)
}

func listenTinyBufferEcho(port int) (net.Listener, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
}

func listenSink(port int) (net.Listener, error) {
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
}

func echoServer(port int, block BlockCrypt) net.Listener {
	l, err := listenEcho(port, block)
	if err != nil {
		panic(err)
		return nil
	}

	go func() {
		kcplistener := l.(*Listener)
		kcplistener.SetReadBuffer(4 * 1024 * 1024)
		kcplistener.SetWriteBuffer(4 * 1024 * 1024)
		kcplistener.SetDSCP(46)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}

			// coverage test
			s.(*UDPSession).SetReadBuffer(4 * 1024 * 1024)
			s.(*UDPSession).SetWriteBuffer(4 * 1024 * 1024)
			go handleEcho(s.(*UDPSession))
		}
	}()

	return l
}

func sinkServer(port int) net.Listener {
	l, err := listenSink(port)
	if err != nil {
		panic(err)
		return nil
	}

	go func() {
		kcplistener := l.(*Listener)
		kcplistener.SetReadBuffer(4 * 1024 * 1024)
		kcplistener.SetWriteBuffer(4 * 1024 * 1024)
		kcplistener.SetDSCP(46)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}

			go handleSink(s.(*UDPSession))
		}
	}()

	return l
}

func tinyBufferEchoServer(port int) net.Listener {
	l, err := listenTinyBufferEcho(port)
	if err != nil {
		panic(err)
		return nil
	}

	go func() {
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}
			go handleTinyBufferEcho(s.(*UDPSession))
		}
	}()
	return l
}

///////////////////////////

func handleEcho(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(4096, 4096)
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1400)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	conn.SetRateLimit(200 * 1024 * 1024)
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}
}

func handleSink(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(4096, 4096)
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1400)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	buf := make([]byte, 65536)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

func handleTinyBufferEcho(conn *UDPSession) {
	conn.SetStreamMode(true)
	buf := make([]byte, 2)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}
}

///////////////////////////

func TestTimeout(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	buf := make([]byte, 10)

	// timeout
	cli.SetDeadline(time.Now().Add(time.Second))
	<-time.After(2 * time.Second)
	n, err := cli.Read(buf)
	if n != 0 || err == nil {
		t.Fail()
		return
	}
}

func TestSendRecv(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()
	cli.SetWriteDelay(true)
	cli.SetDUP(1)
	const N = 100
	buf := make([]byte, 10)
	for i := range N {
		msg := fmt.Sprintf("hello%v", i)
		cli.Write([]byte(msg))

		n, err := cli.Read(buf)
		if err != nil {
			panic(err)
			return
		}

		if string(buf[:n]) != msg {
			t.Fail()
			return
		}
	}
}

func TestAEADSendRecv(t *testing.T) {
	port := nextPort()
	block1, _ := NewAEADCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewAEADCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()
	cli.SetWriteDelay(true)
	const N = 100
	buf := make([]byte, 10)
	for i := range N {
		msg := fmt.Sprintf("hello%v", i)
		cli.Write([]byte(msg))

		n, err := cli.Read(buf)
		if err != nil {
			panic(err)
			return
		}

		if string(buf[:n]) != msg {
			t.Fail()
			return
		}
	}
}

func TestSendVector(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()
	cli.SetWriteDelay(false)
	const N = 100
	buf := make([]byte, 20)
	v := make([][]byte, 2)
	for i := range N {
		v[0] = fmt.Appendf(nil, "hello%v", i)
		v[1] = fmt.Appendf(nil, "world%v", i)
		msg := fmt.Sprintf("hello%vworld%v", i, i)
		cli.WriteBuffers(v)

		n, err := cli.Read(buf)
		if err != nil {
			panic(err)
			return
		}

		if string(buf[:n]) != msg {
			t.Error(string(buf[:n]), msg)
		}
	}
}

func TestTinyBufferReceiver(t *testing.T) {
	port := nextPort()
	l := tinyBufferEchoServer(port)
	defer l.Close()

	cli, err := dialTinyBufferEcho(port)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	const N = 100
	snd := byte(0)
	fillBuffer := func(buf []byte) {
		for i := range buf {
			buf[i] = snd
			snd++
		}
	}

	rcv := byte(0)
	check := func(buf []byte) bool {
		for i := range buf {
			if buf[i] != rcv {
				return false
			}
			rcv++
		}
		return true
	}
	sndbuf := make([]byte, 7)
	rcvbuf := make([]byte, 7)
	for range N {
		fillBuffer(sndbuf)
		cli.Write(sndbuf)

		n, err := io.ReadFull(cli, rcvbuf)
		if err != nil {
			panic(err)
			return
		}

		if !check(rcvbuf[:n]) {
			t.Fail()
			return
		}
	}
}

func TestClose(t *testing.T) {
	var n int
	var err error

	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	// double close
	cli.Close()
	if cli.Close() == nil {
		t.Fatal("double close misbehavior")
		return
	}

	// write after close
	buf := make([]byte, 10)
	n, err = cli.Write(buf)
	if n != 0 || err == nil {
		t.Fatal("write after close misbehavior")
		return
	}

	// write, close, read, read
	block3, _ := NewSalsa20BlockCrypt(pass)
	cli, err = dialEcho(port, block3)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	if _, err = cli.Write(buf); err != nil {
		t.Fatal("write misbehavior")
		return
	}

	// wait until data arrival
	time.Sleep(2 * time.Second)
	// drain
	cli.Close()
	n, err = io.ReadFull(cli, buf)
	if err != nil {
		t.Fatal("closed conn drain bytes failed", err, n)
		return
	}

	// after drain, read should return error
	n, err = cli.Read(buf)
	if n != 0 || err == nil {
		t.Fatal("write->close->drain->read misbehavior", err, n)
		return
	}
}

func TestParallel1024CLIENT_64BMSG_64CNT(t *testing.T) {
	port := nextPort()
	block, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block)
	defer l.Close()

	var wg sync.WaitGroup
	wg.Add(1024)
	for range 1024 {
		go parallel_client(&wg, port)
	}
	wg.Wait()
}

func parallel_client(wg *sync.WaitGroup, port int) (err error) {
	block, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	err = echo_tester(cli, 64, 64)
	wg.Done()
	return
}

func BenchmarkEchoSpeed4K(b *testing.B) {
	speedclient(b, 4096)
}

func BenchmarkEchoSpeed64K(b *testing.B) {
	speedclient(b, 65536)
}

func BenchmarkEchoSpeed512K(b *testing.B) {
	speedclient(b, 524288)
}

func BenchmarkEchoSpeed1M(b *testing.B) {
	speedclient(b, 1048576)
}

func speedclient(b *testing.B, nbytes int) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	b.ReportAllocs()
	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	if err := echo_tester(cli, nbytes, b.N); err != nil {
		b.Fail()
		return
	}
	b.SetBytes(int64(nbytes))
}

func BenchmarkSinkSpeed4K(b *testing.B) {
	sinkclient(b, 4096)
}

func BenchmarkSinkSpeed64K(b *testing.B) {
	sinkclient(b, 65536)
}

func BenchmarkSinkSpeed256K(b *testing.B) {
	sinkclient(b, 524288)
}

func BenchmarkSinkSpeed1M(b *testing.B) {
	sinkclient(b, 1048576)
}

func sinkclient(b *testing.B, nbytes int) {
	port := nextPort()
	l := sinkServer(port)
	defer l.Close()

	b.ReportAllocs()
	cli, err := dialSink(port)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	sink_tester(cli, nbytes, b.N)
	b.SetBytes(int64(nbytes))
}

func echo_tester(cli net.Conn, msglen, msgcount int) error {
	go func() {
		buf := make([]byte, msglen)
		for range msgcount {
			// send packet
			if _, err := cli.Write(buf); err != nil {
				panic(err)
				return
			}
		}
	}()

	// receive packet
	nrecv := 0
	buf := make([]byte, msglen)
	for {
		n, err := cli.Read(buf)
		if err != nil {
			return err
		}
		nrecv += n
		if nrecv == msglen*msgcount {
			break
		}
	}
	return nil
}

func sink_tester(cli *UDPSession, msglen, msgcount int) error {
	// sender
	buf := make([]byte, msglen)
	for range msgcount {
		if _, err := cli.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func TestSNMP(t *testing.T) {
	t.Log(DefaultSnmp.Copy())
	t.Log(DefaultSnmp.Header())
	t.Log(DefaultSnmp.ToSlice())
	DefaultSnmp.Reset()
	t.Log(DefaultSnmp.ToSlice())
}

func TestListenerClose(t *testing.T) {
	port := nextPort()
	l, err := ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 10, 3)
	if err != nil {
		t.Fail()
		return
	}
	defer l.Close()
	l.SetReadDeadline(time.Now().Add(time.Second))
	l.SetWriteDeadline(time.Now().Add(time.Second))
	l.SetDeadline(time.Now().Add(time.Second))
	time.Sleep(2 * time.Second)
	if _, err := l.Accept(); err == nil {
		t.Fail()
		return
	}

	l.Close()
	fakeaddr, _ := net.ResolveUDPAddr("udp6", "127.0.0.1:1111")
	if l.closeSession(fakeaddr) {
		t.Fail()
		return
	}
}

// A wrapper for net.PacketConn that remembers when Close has been called.
type closedFlagPacketConn struct {
	net.PacketConn
	Closed bool
}

func (c *closedFlagPacketConn) Close() error {
	c.Closed = true
	return c.PacketConn.Close()
}

func newClosedFlagPacketConn(c net.PacketConn) *closedFlagPacketConn {
	return &closedFlagPacketConn{c, false}
}

// Listener should not close a net.PacketConn that it did not create.
// https://github.com/xtaci/kcp-go/issues/165
func TestListenerNonOwnedPacketConn(t *testing.T) {
	// Create a net.PacketConn not owned by the Listener.
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
		return
	}
	defer c.Close()
	// Make it remember when it has been closed.
	pconn := newClosedFlagPacketConn(c)

	l, err := ServeConn(nil, 0, 0, pconn)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed before Listener.Close()")
		return
	}

	err = l.Close()
	if err != nil {
		panic(err)
		return
	}

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed after Listener.Close()")
		return
	}
}

// UDPSession should not close a net.PacketConn that it did not create.
// https://github.com/xtaci/kcp-go/issues/165
func TestUDPSessionNonOwnedPacketConn(t *testing.T) {
	l := sinkServer(0)
	defer l.Close()

	// Create a net.PacketConn not owned by the UDPSession.
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
		return
	}
	defer c.Close()
	// Make it remember when it has been closed.
	pconn := newClosedFlagPacketConn(c)

	client, err := NewConn2(l.Addr(), nil, 0, 0, pconn)
	if err != nil {
		panic(err)
		return
	}
	defer client.Close()

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed before UDPSession.Close()")
		return
	}

	err = client.Close()
	if err != nil {
		panic(err)
		return
	}

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed after UDPSession.Close()")
		return
	}
}

// this function test the data correctness with FEC and encryption enabled
func TestReliability(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()
	cli.SetWriteDelay(false)

	const N = 100000
	buf := make([]byte, 128)
	msg := make([]byte, 128)

	for range N {
		io.ReadFull(rand.Reader, msg)
		cli.Write([]byte(msg))

		n, err := io.ReadFull(cli, buf)
		if err != nil {
			panic(err)
			return
		}

		if !bytes.Equal(buf[:n], msg) {
			t.Fail()
			return
		}
	}
}

func TestControl(t *testing.T) {
	port := nextPort()
	block, _ := NewSalsa20BlockCrypt(pass)
	l, err := ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 1)
	if err != nil {
		panic(err)
		return
	}
	defer l.Close()

	errorA := errors.New("A")
	err = l.Control(func(conn net.PacketConn) error {
		fmt.Printf("Listener Control: conn: %v\n", conn)
		return errorA
	})

	if err != errorA {
		t.Fatal(err)
		return
	}

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	errorB := errors.New("B")
	err = cli.Control(func(conn net.PacketConn) error {
		fmt.Printf("Client Control: conn: %v\n", conn)
		return errorB
	})

	if err != errorB {
		t.Fatal(err)
		return
	}
}

func TestSessionReadAfterClosed(t *testing.T) {
	us, _ := net.ListenPacket("udp", "127.0.0.1:0")
	uc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer us.Close()
	defer uc.Close()

	knockDoor := func(c net.Conn, myid string) (string, error) {
		c.SetDeadline(time.Now().Add(time.Second * 3))
		_, err := c.Write([]byte(myid))
		c.SetDeadline(time.Time{})
		if err != nil {
			return "", err
		}
		c.SetDeadline(time.Now().Add(time.Second * 3))
		var buf [1024]byte
		n, err := c.Read(buf[:])
		c.SetDeadline(time.Time{})
		return string(buf[:n]), err
	}

	check := func(c1, c2 *UDPSession) {
		done := make(chan struct{}, 1)
		go func() {
			rid, err := knockDoor(c2, "4321")
			done <- struct{}{}
			if err != nil {
				panic(err)
				return
			}

			if rid != "1234" {
				panic("mismatch id")
				return
			}
		}()

		rid, err := knockDoor(c1, "1234")
		if err != nil {
			panic(err)
			return
		}

		if rid != "4321" {
			panic("mismatch id")
			return
		}
		<-done
	}

	c1, err := NewConn3(0, uc.LocalAddr(), nil, 0, 0, us)
	if err != nil {
		panic(err)
		return
	}
	defer c1.Close()

	c2, err := NewConn3(0, us.LocalAddr(), nil, 0, 0, uc)
	if err != nil {
		panic(err)
		return
	}
	defer c2.Close()

	check(c1, c2)
	c1.Close()
	c2.Close()
	// log.Println("conv id 0 is closed")

	c1, err = NewConn3(4321, uc.LocalAddr(), nil, 0, 0, us)
	if err != nil {
		panic(err)
		return
	}
	defer c1.Close()

	c2, err = NewConn3(4321, us.LocalAddr(), nil, 0, 0, uc)
	if err != nil {
		panic(err)
		return
	}
	defer c2.Close()

	check(c1, c2)
	c1.Close()
	c2.Close()
}

func TestSetMTU(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	ok := cli.SetMtu(49)
	if ok {
		t.Fatal("can not set mtu small than 50")
		return
	}

	cli.SetMtu(1500)
	cli.SetWriteDelay(false)
	cli.SetLogger(IKCP_LOG_ALL, newLoggerWithMilliseconds().Info)

	sendBytes := make([]byte, 1500)
	rand.Read(sendBytes)
	cli.Write(sendBytes)

	buf := make([]byte, 1500)

	n, err := io.ReadFull(cli, buf)
	if err != nil {
		panic(err)
		return
	}

	if !bytes.Equal(buf[:n], sendBytes) {
		t.Fail()
		return
	}
}

func newLoggerWithMilliseconds() *slog.Logger {
	timeFormat := "2006-01-02 15:04:05.000"
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(time.Now().Format(timeFormat))
			}
			return a
		},
	})
	return slog.New(handler)
}

// TestSetLogger if want logging kcp trace need set build tags with debug
// trace log on:
//
//	go test -run ^TestSetLogger$ -tags debug
//
// trace log off:
//
//	go test -run ^TestSetLogger$
func TestSetLogger(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
		return
	}
	defer cli.Close()

	cli.SetWriteDelay(true)
	cli.SetDUP(1)
	cli.SetLogger(IKCP_LOG_ALL, newLoggerWithMilliseconds().Info)
	const N = 10
	buf := make([]byte, 10)
	for i := range N {
		msg := fmt.Sprintf("trace%v", i)
		cli.Write([]byte(msg))

		n, err := cli.Read(buf)
		if err != nil {
			panic(err)
			return
		}

		if string(buf[:n]) != msg {
			t.Fail()
			return
		}
	}
}
