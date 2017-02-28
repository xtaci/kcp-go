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
		log.Println("listening on:", kcplistener.conn.LocalAddr())
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
	log.Println("new client", conn.RemoteAddr())
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
	var wg sync.WaitGroup
	const par = 1
	wg.Add(par)
	for i := 0; i < par; i++ {
		go sendrecvclient(&wg)
	}
	wg.Wait()
}

func sendrecvclient(wg *sync.WaitGroup) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	cli.SetReadBuffer(4 * 1024 * 1024)
	cli.SetWriteBuffer(4 * 1024 * 1024)
	cli.SetStreamMode(true)
	cli.SetNoDelay(1, 20, 2, 1)
	cli.SetACKNoDelay(true)
	cli.SetDeadline(time.Now().Add(time.Minute))
	const N = 100
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		log.Println("sent:", msg)
		cli.Write([]byte(msg))
		if n, err := cli.Read(buf); err == nil {
			log.Println("recv:", string(buf[:n]))
		} else {
			panic(err)
		}
	}
	wg.Done()
	wg.Wait() // make sure ports not reused
	cli.Close()
}

func TestBigPacket(t *testing.T) {
	l := server()
	defer l.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go bigclient(&wg)
	wg.Wait()
}

func bigclient(wg *sync.WaitGroup) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	cli.SetNoDelay(1, 20, 2, 1)
	const N = 10
	buf := make([]byte, 1024*512)
	msg := make([]byte, 1024*512)

	total := len(msg) * N
	println("total to send:", total)
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
	wg.Done()
	cli.Close()
}

func TestSpeed(t *testing.T) {
	l := server()
	defer l.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go speedclient(&wg)
	wg.Wait()
}

func speedclient(wg *sync.WaitGroup) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	log.Println("remote:", cli.RemoteAddr(), "local:", cli.LocalAddr())
	log.Println("conv:", cli.GetConv())
	cli.SetNoDelay(1, 20, 2, 1)
	start := time.Now()

	go func() {
		buf := make([]byte, 1024*1024)
		nrecv := 0
		for {
			n, err := cli.Read(buf)
			if err != nil {
				log.Println(err)
				break
			} else {
				nrecv += n
				if nrecv == 4096*4096 {
					break
				}
			}
		}
		println("total recv:", nrecv)
		log.Println("time for 16MB rtt with encryption", time.Now().Sub(start))
		log.Printf("%+v\n", DefaultSnmp.Copy())
		log.Println(DefaultSnmp.Header())
		log.Println(DefaultSnmp.ToSlice())
		DefaultSnmp.Reset()
		log.Println(DefaultSnmp.ToSlice())
		wg.Done()
	}()
	msg := make([]byte, 4096)
	cli.SetWindowSize(1024, 1024)
	for i := 0; i < 4096; i++ {
		cli.Write(msg)
	}
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

func TestParallel(t *testing.T) {
	l := server()
	defer l.Close()
	par := 1000
	var wg sync.WaitGroup
	wg.Add(par)
	log.Println("testing parallel", par, "connections")
	for i := 0; i < par; i++ {
		go parallelclient(&wg)
	}
	wg.Wait()
}

func parallelclient(wg *sync.WaitGroup) {
	cli, err := DialTest()
	if err != nil {
		panic(err)
	}
	const N = 100
	cli.SetNoDelay(1, 20, 2, 1)
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		cli.Write([]byte(msg))
		if _, err := cli.Read(buf); err != nil {
			break
		}
		<-time.After(10 * time.Millisecond)
	}
	wg.Done()
	wg.Wait() // make sure ports not reused
	cli.Close()
}
