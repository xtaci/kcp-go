package kcp

import (
	"fmt"
	"net"
	"sync"
	"testing"
)

const port = "127.0.0.1:9999"

func init() {
	go server()
	println("init")
}

func server() {
	l, err := Listen(port)
	if err != nil {
		panic(err)
	}
	for {
		fmt.Println("accept loop")
		s, err := l.Accept()
		if err != nil {
			panic(err)
		}

		go handle_client(s)
	}
}

func handle_client(conn net.Conn) {
	fmt.Println("new client", conn)
	buf := make([]byte, 10)
	for {
		n, err := conn.Read(buf)
		fmt.Println("receiving:", string(buf[:n]))
		if err != nil {
			panic(err)
		}
		conn.Write(buf[:n])
	}
}

func TestSess(t *testing.T) {
	var wg sync.WaitGroup
	const par = 10
	wg.Add(par)
	for i := 0; i < par; i++ {
		go client(&wg)
	}
	wg.Wait()
}

func client(wg *sync.WaitGroup) {
	cli, err := Dial(port)
	if err != nil {
		panic(err)
	}
	const N = 10
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		fmt.Println("sendmsg:", i)
		cli.Write([]byte(fmt.Sprintf("hello%v", i)))
		_, err := cli.Read(buf)
		if err != nil {
			panic(err)
		}
	}
	wg.Done()
}
