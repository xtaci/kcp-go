package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/proxy"
)

func echoTester(c net.Conn, msglen, interval int) (err error) {
	time.Sleep(time.Second)
	buf := make([]byte, msglen)
	for {
		// send packet
		start := time.Now()
		_, err = c.Write(buf)
		if err != nil {
			return err
		}

		// receive packet
		nrecv := 0
		var n int
		for {
			n, err = c.Read(buf)
			if err != nil {
				return err
			}
			nrecv += n
			if nrecv == msglen {
				break
			}
		}
		costTmp := time.Since(start)
		cost := float64(costTmp.Nanoseconds()) / (1000 * 1000)
		if interval != 0 {
			time.Sleep(time.Millisecond * time.Duration(interval))
		}
		fmt.Printf("echo now:%v cost:%v\n", time.Now(), cost)
	}
	return nil
}

var proxyAddr = flag.String("proxyAddr", ":9090", "socks5 proxy addr")
var targetAddr = flag.String("targetAddr", ":7900", "echo server addr")
var msglen = flag.Int("msglen", 100, "input msg length")
var interval = flag.Int("interval", 0, "echo interval")

func main() {
	a := []int{1, 2, 3, 4, 5}
	fmt.Println(a, len(a), cap(a))
	b := a[1:]
	fmt.Println(b, len(b), cap(b))
	b = append(b, 10)
	fmt.Println(a, len(a), cap(a))
	fmt.Println(b, len(b), cap(b))
	c := b[0:8]
	fmt.Println(c, len(c), cap(c))
	return

	flag.Parse()
	fmt.Printf("proxyAddr:%v\n", *proxyAddr)
	fmt.Printf("targetAddr:%v\n", *targetAddr)

	s5, _ := proxy.SOCKS5("tcp", *proxyAddr, nil, &net.Dialer{})
	conn, err := s5.Dial("tcp", *targetAddr)
	if err != nil {
		fmt.Println("socks5 Dial failure")
		return
	}

	echoTester(conn, *msglen, *interval)
}
