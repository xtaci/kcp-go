package kcp

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

const port = "127.0.0.1:9999"
const key = "testkey"

func server() {
	l, err := ListenEncrypted(MODE_NORMAL, port, key)
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

func handle_client(conn *UDPSession) {
	conn.SetWindowSize(1024, 1024)
	fmt.Println("new client", conn.RemoteAddr())
	buf := make([]byte, 65536)
	count := 0
	for {
		n, err := conn.Read(buf)
		if err != nil {
			panic(err)
		}
		count++
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
	cli, err := DialEncrypted(MODE_NORMAL, port, key)
	if err != nil {
		panic(err)
	}
	const N = 100
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

func TestBigPacket(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go client2(&wg)
	wg.Wait()
}

func client2(wg *sync.WaitGroup) {
	cli, err := DialEncrypted(MODE_NORMAL, port, key)
	if err != nil {
		panic(err)
	}
	const N = 10
	buf := make([]byte, 1024*512)
	msg := make([]byte, 1024*512)
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

func TestSpeed(t *testing.T) {
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(1)
	go client3(&wg)
	wg.Wait()
	fmt.Println("time for 1MB rtt", time.Now().Sub(start))
}

func client3(wg *sync.WaitGroup) {
	cli, err := DialEncrypted(MODE_NORMAL, port, key)
	if err != nil {
		panic(err)
	}
	msg := make([]byte, 1024*1024)
	buf := make([]byte, 65536)
	cli.SetWindowSize(1024, 1024)
	cli.Write(msg)
	nrecv := 0
	for {
		n, err := cli.Read(buf)
		if err != nil {
			fmt.Println(err)
			break
		} else {
			nrecv += n
			if nrecv == 1024*1024 {
				break
			}
		}
	}

	println("total recv:", nrecv)
	cli.Close()
	wg.Done()
}

func TestParallel(t *testing.T) {
	par := 200
	var wg sync.WaitGroup
	wg.Add(par)
	fmt.Println("testing parallel", par, "connections")
	for i := 0; i < par; i++ {
		go client4(&wg)
	}
	wg.Wait()
}

func client4(wg *sync.WaitGroup) {
	cli, err := DialEncrypted(MODE_NORMAL, port, key)
	if err != nil {
		panic(err)
	}
	const N = 1000
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		cli.Write([]byte(msg))
		if _, err := cli.Read(buf); err != nil {
			break
		}
	}
	cli.Close()
	wg.Done()
}
