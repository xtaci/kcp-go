package main

import (
	"flag"
	"fmt"
	"net"
	"strconv"
	"time"
)

var listenAddr = flag.String("listenAddr", "127.0.0.1:7900", "listen address")
var targetAddr = flag.String("targetAddr", "127.0.0.1:7900", "target address")

func st(t *[]int) {
	*t = append(*t, 1)
}

type A struct {
	a int
	b []int
}

func (a *A) test() {
	fmt.Println("A::test")
}

type AA interface {
	test()
}

func main() {
	a := &A{}
	var aa AA = a
	var bb AA = a
	fmt.Println(aa == bb)

	return

	// var nc net.Conn
	var conns map[net.Conn]int
	conns[nil] = 1
	delete(conns, nil)
	fmt.Println(len(conns))
	switch conns[nil] {
	case 0:
		fmt.Println(0)
	default:
		fmt.Println(1)
	}
	return

	var respHeaderTimer <-chan time.Time
	go func() {
		time.Sleep(time.Second * 3)
		fmt.Println("time sleep finish")
		t := time.NewTimer(time.Second)
		t.Reset(time.Second)
		respHeaderTimer = t.C

		time.Sleep(time.Second * 6)
		fmt.Println("time sleep 6 second finish")
		t.Reset(time.Second)
		respHeaderTimer = t.C
	}()

	for {
		select {
		case <-respHeaderTimer:
			fmt.Println("time expire")
			respHeaderTimer = nil
		case <-time.After(time.Second):
			fmt.Println("time after")
		}
	}

	return

	flag.Parse()

	fmt.Printf("listenAddr:%v\n", *listenAddr)
	fmt.Printf("targetAddr:%v\n", *targetAddr)

	addr, err := net.ResolveUDPAddr("udp", *listenAddr)
	if err != nil {
		fmt.Println("ResolveUDPAddr error:", err)
		return
	}

	network := "udp4"
	if addr.IP.To4() == nil {
		network = "udp"
	}

	conn, err := net.ListenUDP(network, addr)
	if err != nil {
		fmt.Println("ListenUDP error:", addr, err)
		return
	}

	buf := make([]byte, 1024)
	for {
		if n, from, err := conn.ReadFrom(buf); err == nil {
			s, err := strconv.Atoi(string(buf[:n]))
			if err != nil {
				fmt.Println("read atoi err:", err)
				continue
			}
			fmt.Printf("read from:%v s:%v \n", from, s)
			_, err = conn.WriteTo(buf[:n], from)
			if err != nil {
				fmt.Println("read and write err:", err)
				continue
			}
		} else {
			fmt.Println("read error:", err)
		}
	}
}
