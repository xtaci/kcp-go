package kcp

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

const port = "127.0.0.1:9999"

func server() {
	l, err := Listen(MODE_NORMAL, port)
	if err != nil {
		panic(err)
	}
	for {
		s, err := l.Accept()
		if err != nil {
			panic(err)
		}

		go handle_client(s)
	}
}

func init() {
	go server()
}

func handle_client(conn net.Conn) {
	fmt.Println("new client", conn.RemoteAddr())
	buf := make([]byte, 10)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			panic(err)
		}
		fmt.Println("recv:", string(buf[:n]))
		conn.Write(buf[:n])
	}
}

func TestSendRecv(t *testing.T) {
	var wg sync.WaitGroup
	const par = 10
	wg.Add(par)
	for i := 0; i < par; i++ {
		go client(&wg)
	}
	wg.Wait()
}

func client(wg *sync.WaitGroup) {
	cli, err := Dial(MODE_NORMAL, port)
	if err != nil {
		panic(err)
	}
	const N = 10
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		fmt.Println("sent:", msg)
		cli.Write([]byte(msg))
		if n, err := cli.Read(buf); err == nil {
			fmt.Println("recv:", string(buf[:n]))
		} else {
			panic(err)
		}
	}
	cli.Close()
	wg.Done()
}

func TestListen(t *testing.T) {
	l, err := Listen(MODE_NORMAL, port)
	if err != nil {
		panic(err)
	}
	l.Close()
}

func TestBigPacket(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go client2(&wg)
	wg.Wait()
}

func client2(wg *sync.WaitGroup) {
	cli, err := Dial(MODE_NORMAL, port)
	if err != nil {
		panic(err)
	}
	const N = 10
	buf := make([]byte, 100)
	msg := make([]byte, 4096)
	for i := 0; i < N; i++ {
		cli.Write(msg)

	}
	println("total written:", len(msg)*N)

	nrecv := 0
	cli.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		n, err := cli.Read(buf)
		if err != nil {
			break
		} else {
			nrecv += n
			if nrecv == len(msg)*N {
				break
			}
		}
	}

	println("total recv:", nrecv)
	cli.Close()
	wg.Done()
}
