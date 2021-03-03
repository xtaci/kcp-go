package main

import (
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type batchConn interface {
	WriteBatch(ms []ipv4.Message, flags int) (int, error)
	ReadBatch(ms []ipv4.Message, flags int) (int, error)
}

var listenAddr = flag.String("listenAddr", "127.0.0.1:7900", "listen address")
var targetAddr = flag.String("targetAddr", "127.0.0.1:7900", "target address")

func main() {
	flag.Parse()

	fmt.Printf("listenAddr:%v\n", *listenAddr)
	fmt.Printf("targetAddr:%v\n", *targetAddr)

	addr, err := net.ResolveUDPAddr("udp", *listenAddr)
	if err != nil {
		fmt.Println("ResolveUDPAddr error:", err)
		return
	}

	addrs := strings.Split(*targetAddr, ",")
	remoteAddrs := make([]*net.UDPAddr, len(addrs))
	for i := 0; i < len(addrs); i++ {
		remoteAddr, err := net.ResolveUDPAddr("udp", addrs[i])
		if err != nil {
			fmt.Println("ResolveUDPAddr error:", err)
			return
		}
		remoteAddrs[i] = remoteAddr
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

	var xconn batchConn // for x/net
	if addr.IP.To4() != nil {
		xconn = ipv4.NewPacketConn(conn)
	} else {
		xconn = ipv6.NewPacketConn(conn)
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
			time.Sleep(time.Millisecond * 500)

			mu.Lock()
			start += 1
			session[start] = time.Now()
			mu.Unlock()

			str := strconv.Itoa(start)

			msgs := make([]ipv4.Message, len(remoteAddrs))
			for i := 0; i < len(remoteAddrs); i++ {
				msg := ipv4.Message{}
				msg.Buffers = [][]byte{[]byte(str)}
				msg.Addr = remoteAddrs[i]
				msgs[i] = msg
			}

			n, err := xconn.WriteBatch(msgs, 0)
			if err != nil {
				fmt.Printf("write batch failed. err:%v \n", err)
				continue
			}

			if n != len(remoteAddrs) {
				panic("n is not equal len remote addrs")
			}
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
