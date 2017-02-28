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

	go server()
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

func server() {
	l, err := ListenTest()
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
			s.(*UDPSession).SetKeepAlive(1)
			go handleClient(s.(*UDPSession))
		}
	}()
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

func TestClose(t *testing.T) {
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

func TestParallel1024CLIENT_64BMSG_64CNT(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1024)
	for i := 0; i < 1024; i++ {
		go parallel_client(&wg)
	}
	wg.Wait()
}

func parallel_client(wg *sync.WaitGroup) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}

	echo_tester(cli, 64, 64)
	wg.Done()
}

func BenchmarkSpeed4K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		speedclient(b, 4096)
	}
}

func BenchmarkSpeed64K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		speedclient(b, 65536)
	}
}

func BenchmarkSpeed512K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		speedclient(b, 524288)
	}
}

func BenchmarkSpeed1M(b *testing.B) {
	for i := 0; i < b.N; i++ {
		speedclient(b, 1048576)
	}
}

func speedclient(b *testing.B, nbytes int) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}

	echo_tester(cli, nbytes, 1)
	b.SetBytes(int64(nbytes) * 4)
}

func TestSNMP(t *testing.T) {
	t.Log(DefaultSnmp.Copy())
	t.Log(DefaultSnmp.Header())
	t.Log(DefaultSnmp.ToSlice())
	DefaultSnmp.Reset()
	t.Log(DefaultSnmp.ToSlice())
}

func echo_tester(cli net.Conn, msglen, msgcount int) {
	total := msglen * msgcount

	var wg sync.WaitGroup

	// sender
	wg.Add(2)
	go func() {
		buf := make([]byte, msglen)
		for i := 0; i < msgcount; i++ {
			cli.Write(buf)
		}
		wg.Done()
	}()

	// receiver
	go func() {
		buf := make([]byte, msglen)
		nrecv := 0
		for {
			n, err := cli.Read(buf)
			if err != nil {
				log.Println(err)
				break
			} else {
				nrecv += n
				if nrecv == total {
					break
				}
			}
		}
		wg.Done()
	}()
	wg.Wait()
}
