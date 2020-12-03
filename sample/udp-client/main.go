package main

import (
	"flag"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

var listenAddr = flag.String("listenAddr", "127.0.0.1:7900", "listen address")
var targetAddr = flag.String("targetAddr", "127.0.0.1:7900", "target address")

type xxx struct {
	a int
	b string
}

func (x *xxx) String() string {
	return "a" + x.b
}

func main() {
	b := -3
	a := make([]int, 3)
	c := a[b:]
	fmt.Println(len(c))
	return
	flag.Parse()

	fmt.Printf("listenAddr:%v\n", *listenAddr)
	fmt.Printf("targetAddr:%v\n", *targetAddr)

	addr, err := net.ResolveUDPAddr("udp", *listenAddr)
	if err != nil {
		fmt.Println("ResolveUDPAddr error:", err)
		return
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", *targetAddr)
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

	session := make(map[int]time.Time)
	start := 0

	mu := &sync.Mutex{}

	go func() {
		for {
			time.Sleep(time.Second)

			mu.Lock()
			packetLoss := len(session)
			packeCount := start
			mu.Unlock()

			fmt.Printf("sendCount:%v loss:%v \n", packeCount, packetLoss)
		}
	}()

	go func() {
		for {
			mu.Lock()
			start += 1
			session[start] = time.Now()
			mu.Unlock()

			str := strconv.Itoa(start)

			_, err := conn.WriteTo([]byte(str), remoteAddr)
			if err != nil {
				fmt.Println("write error:", err)
			}

			time.Sleep(time.Millisecond * 500)
		}
	}()

	buf := make([]byte, 1024)
	for {
		if n, from, err := conn.ReadFrom(buf); err == nil {
			s, err := strconv.Atoi(string(buf[:n]))
			if err != nil {
				fmt.Println("read atoi err:", err)
				continue
			}
			mu.Lock()
			pass := time.Since(session[s])
			delete(session, s)
			mu.Unlock()

			fmt.Printf("read pass from:%v pass:%v \n", from, pass)
		} else {
			fmt.Println("read error:", err)
		}
	}
}
