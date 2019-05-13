package kcp

import (
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

var baseport = uint32(10000)
var key = []byte("testkey")
var pass = pbkdf2.Key(key, []byte("testsalt"), 4096, 32, sha1.New)

func init() {
	go func() {
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()

	log.Println("beginning tests, encryption:salsa20, fec:10/3")
}

func dialEcho(port int) (*UDPSession, error) {
	//block, _ := NewNoneBlockCrypt(pass)
	//block, _ := NewSimpleXORBlockCrypt(pass)
	//block, _ := NewTEABlockCrypt(pass[:16])
	//block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetStreamMode(false)
	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetStreamMode(true)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetMtu(1600)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(true)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, err
}

func dialSink(port int) (*UDPSession, error) {
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetStreamMode(true)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, err
}

func dialTinyBufferEcho(port int) (*UDPSession, error) {
	//block, _ := NewNoneBlockCrypt(pass)
	//block, _ := NewSimpleXORBlockCrypt(pass)
	//block, _ := NewTEABlockCrypt(pass[:16])
	//block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
	if err != nil {
		panic(err)
	}
	return sess, err
}

//////////////////////////
func listenEcho(port int) (net.Listener, error) {
	//block, _ := NewNoneBlockCrypt(pass)
	//block, _ := NewSimpleXORBlockCrypt(pass)
	//block, _ := NewTEABlockCrypt(pass[:16])
	//block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
}
func listenTinyBufferEcho(port int) (net.Listener, error) {
	//block, _ := NewNoneBlockCrypt(pass)
	//block, _ := NewSimpleXORBlockCrypt(pass)
	//block, _ := NewTEABlockCrypt(pass[:16])
	//block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
}

func listenSink(port int) (net.Listener, error) {
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
}

func echoServer(port int) net.Listener {
	l, err := listenEcho(port)
	if err != nil {
		panic(err)
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
	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}
	buf := make([]byte, 10)

	//timeout
	cli.SetDeadline(time.Now().Add(time.Second))
	<-time.After(2 * time.Second)
	n, err := cli.Read(buf)
	if n != 0 || err == nil {
		t.Fail()
	}
	cli.Close()
}

func TestSendRecv(t *testing.T) {
	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}
	cli.SetWriteDelay(true)
	cli.SetDUP(1)
	const N = 100
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		cli.Write([]byte(msg))
		if n, err := cli.Read(buf); err == nil {
			if string(buf[:n]) != msg {
				t.Fail()
			}
		} else {
			panic(err)
		}
	}
	cli.Close()
}

func TestSendVector(t *testing.T) {
	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}
	cli.SetWriteDelay(false)
	const N = 100
	buf := make([]byte, 20)
	v := make([][]byte, 2)
	for i := 0; i < N; i++ {
		v[0] = []byte(fmt.Sprintf("hello%v", i))
		v[1] = []byte(fmt.Sprintf("world%v", i))
		msg := fmt.Sprintf("hello%vworld%v", i, i)
		cli.WriteBuffers(v)
		if n, err := cli.Read(buf); err == nil {
			if string(buf[:n]) != msg {
				t.Error(string(buf[:n]), msg)
			}
		} else {
			panic(err)
		}
	}
	cli.Close()
}

func TestTinyBufferReceiver(t *testing.T) {
	port := int(atomic.AddUint32(&baseport, 1))
	l := tinyBufferEchoServer(port)
	defer l.Close()

	cli, err := dialTinyBufferEcho(port)
	if err != nil {
		panic(err)
	}
	const N = 100
	snd := byte(0)
	fillBuffer := func(buf []byte) {
		for i := 0; i < len(buf); i++ {
			buf[i] = snd
			snd++
		}
	}

	rcv := byte(0)
	check := func(buf []byte) bool {
		for i := 0; i < len(buf); i++ {
			if buf[i] != rcv {
				return false
			}
			rcv++
		}
		return true
	}
	sndbuf := make([]byte, 7)
	rcvbuf := make([]byte, 7)
	for i := 0; i < N; i++ {
		fillBuffer(sndbuf)
		cli.Write(sndbuf)
		if n, err := io.ReadFull(cli, rcvbuf); err == nil {
			if !check(rcvbuf[:n]) {
				t.Fail()
			}
		} else {
			panic(err)
		}
	}
	cli.Close()
}

func TestClose(t *testing.T) {
	var n int
	var err error

	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}

	// double close
	cli.Close()
	if cli.Close() == nil {
		t.Fatal("double close misbehavior")
	}

	// write after close
	buf := make([]byte, 10)
	n, err = cli.Write(buf)
	if n != 0 || err == nil {
		t.Fatal("write after close misbehavior")
	}

	// write, close, read, read
	cli, err = dialEcho(port)
	if err != nil {
		panic(err)
	}
	if n, err = cli.Write(buf); err != nil {
		t.Fatal("write misbehavior")
	}

	// wait until data arrival
	time.Sleep(2 * time.Second)
	// drain
	cli.Close()
	n, err = io.ReadFull(cli, buf)
	if err != nil {
		t.Fatal("closed conn drain bytes failed", err, n)
	}

	// after drain, read should return error
	n, err = cli.Read(buf)
	if n != 0 || err == nil {
		t.Fatal("write->close->drain->read misbehavior", err, n)
	}
	cli.Close()
}

func TestParallel1024CLIENT_64BMSG_64CNT(t *testing.T) {
	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	var wg sync.WaitGroup
	wg.Add(1024)
	for i := 0; i < 1024; i++ {
		go parallel_client(&wg, port)
	}
	wg.Wait()
}

func parallel_client(wg *sync.WaitGroup, port int) (err error) {
	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}

	err = echo_tester(cli, 64, 64)
	cli.Close()
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
	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	b.ReportAllocs()
	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}

	if err := echo_tester(cli, nbytes, b.N); err != nil {
		b.Fail()
	}
	b.SetBytes(int64(nbytes))
	cli.Close()
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
	port := int(atomic.AddUint32(&baseport, 1))
	l := sinkServer(port)
	defer l.Close()

	b.ReportAllocs()
	cli, err := dialSink(port)
	if err != nil {
		panic(err)
	}

	sink_tester(cli, nbytes, b.N)
	b.SetBytes(int64(nbytes))
	cli.Close()
}

func echo_tester(cli net.Conn, msglen, msgcount int) error {
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		// send packet
		if _, err := cli.Write(buf); err != nil {
			return err
		}

		// receive packet
		nrecv := 0
		for {
			n, err := cli.Read(buf)
			if err != nil {
				return err
			} else {
				nrecv += n
				if nrecv == msglen {
					break
				}
			}
		}
	}
	return nil
}

func sink_tester(cli *UDPSession, msglen, msgcount int) error {
	// sender
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
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
	port := int(atomic.AddUint32(&baseport, 1))
	l, err := ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 10, 3)
	if err != nil {
		t.Fail()
	}
	l.SetReadDeadline(time.Now().Add(time.Second))
	l.SetWriteDeadline(time.Now().Add(time.Second))
	l.SetDeadline(time.Now().Add(time.Second))
	time.Sleep(2 * time.Second)
	if _, err := l.Accept(); err == nil {
		t.Fail()
	}

	l.Close()
	fakeaddr, _ := net.ResolveUDPAddr("udp6", "127.0.0.1:1111")
	if l.closeSession(fakeaddr) {
		t.Fail()
	}
}
