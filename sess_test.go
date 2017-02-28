package kcp

import (
	"crypto/sha1"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const port = "127.0.0.1:9999"
const salt = "kcptest"

var key = []byte("testkey")
var fec = 4
var pass = pbkdf2.Key(key, []byte(salt), 4096, 32, sha1.New)

func init() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

}

func DialTest() (*UDPSession, error) {
	block, _ := NewNoneBlockCrypt(pass)
	//block, _ := NewSimpleXORBlockCrypt(pass)
	//block, _ := NewTEABlockCrypt(pass[:16])
	//block, _ := NewAESBlockCrypt(pass)
	sess, err := DialWithOptions(port, block, 10, 3)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(4 * 1024 * 1024)
	sess.SetWriteBuffer(4 * 1024 * 1024)
	sess.SetStreamMode(true)
	sess.SetNoDelay(1, 20, 2, 1)
	sess.SetACKNoDelay(true)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, err
}

func ListenTest() (net.Listener, error) {
	block, _ := NewNoneBlockCrypt(pass)
	//block, _ := NewSimpleXORBlockCrypt(pass)
	//block, _ := NewTEABlockCrypt(pass[:16])
	//block, _ := NewAESBlockCrypt(pass)
	return ListenWithOptions(port, block, 10, 3)
}

func server() net.Listener {
	l, err := ListenTest()
	for {
		if err != nil {
			<-time.After(time.Second)
			l, err = ListenTest()
		} else {
			break
		}
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
			s.(*UDPSession).SetKeepAlive(1)
			go handleClient(s.(*UDPSession))
		}
	}()
	return l
}

func handleClient(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(1024, 1024)
	conn.SetNoDelay(1, 20, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1450)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			panic(err)
		}
		conn.Write(buf[:n])
	}
}

func TestTimeout(t *testing.T) {
	l := server()
	defer l.Close()

	cli, err := DialTest()
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
	n, err = cli.Write(buf)
	if n != 0 || err == nil {
		t.Fail()
	}
	cli.Close()
}

func TestSendRecv(t *testing.T) {
	l := server()
	defer l.Close()
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
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

func TestBigPacket(t *testing.T) {
	l := server()
	defer l.Close()

	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	cli.SetNoDelay(1, 20, 2, 1)
	const N = 10
	buf := make([]byte, 1024*512)
	msg := make([]byte, 1024*512)

	total := len(msg) * N
	var receiverWaiter sync.WaitGroup
	receiverWaiter.Add(1)

	go func() {
		nrecv := 0
		for {
			n, err := cli.Read(buf)
			if err != nil {
				break
			} else {
				nrecv += n
				if nrecv == total {
					receiverWaiter.Done()
					return
				}
			}
		}
	}()

	for i := 0; i < N; i++ {
		cli.Write(msg)
	}
	receiverWaiter.Wait()
	cli.Close()
}

func TestClose(t *testing.T) {
	l := server()
	defer l.Close()

	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	buf := make([]byte, 10)

	cli.Close()
	if cli.Close() == nil {
		t.Fail()
	}
	n, err := cli.Write(buf)
	if n != 0 || err == nil {
		t.Fail()
	}
	n, err = cli.Read(buf)
	if n != 0 || err == nil {
		t.Fail()
	}
	cli.Close()
}

func BenchmarkParallel100x10(b *testing.B) {
	l := server()
	defer l.Close()
	var wg sync.WaitGroup
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go parallelclient(&wg, b, 100, 10)
	}
	wg.Wait()
}

func BenchmarkParallel100x100(b *testing.B) {
	l := server()
	defer l.Close()
	var wg sync.WaitGroup
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go parallelclient(&wg, b, 100, 100)
	}
	wg.Wait()
}

func BenchmarkParallel100x1000(b *testing.B) {
	l := server()
	defer l.Close()
	var wg sync.WaitGroup
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go parallelclient(&wg, b, 100, 1000)
	}
	wg.Wait()
}

func parallelclient(wg *sync.WaitGroup, b *testing.B, count, nbytes int) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	cli.SetNoDelay(1, 20, 2, 1)
	buf := make([]byte, nbytes)
	for i := 0; i < count; i++ {
		msg := fmt.Sprintf("hello%v", i)
		cli.Write([]byte(msg))
		if _, err := cli.Read(buf); err != nil {
			break
		}
	}
	b.SetBytes(int64(count * nbytes))
	wg.Done()
	wg.Wait() // make sure ports not reused
}

func BenchmarkSpeed(b *testing.B) {
	l := server()
	defer l.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go speedclient(&wg, b)
	wg.Wait()
}

func speedclient(wg *sync.WaitGroup, b *testing.B) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	cli.SetNoDelay(1, 20, 2, 1)

	// pong
	go func() {
		buf := make([]byte, 1024*1024)
		nrecv := 0
		for {
			n, err := cli.Read(buf)
			if err != nil {
				break
			} else {
				nrecv += n
				if nrecv == 4096*4096 {
					break
				}
			}
		}
		wg.Done()
	}()

	//ping
	msg := make([]byte, 4096)
	for i := 0; i < 4096; i++ {
		cli.Write(msg)
	}
	cli.Close()
	wg.Wait()
	b.SetBytes(4096 * 4096)
}

func TestSNMP(t *testing.T) {
	t.Log(DefaultSnmp.Copy())
	t.Log(DefaultSnmp.Header())
	t.Log(DefaultSnmp.ToSlice())
	DefaultSnmp.Reset()
	t.Log(DefaultSnmp.ToSlice())
}
