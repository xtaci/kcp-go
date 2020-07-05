package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"
)

func checkError(err error) {
	if err != nil {
		fmt.Println("checkError", err)
		os.Exit(-1)
	}
}

type client struct {
	*net.TCPConn
	count int
	cost  float64
}

func echoTester(c *client, msglen, msgcount int) error {
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		// send packet
		start := time.Now()
		_, err := c.Write(buf)
		if err != nil {
			return err
		}

		// receive packet
		nrecv := 0
		for {
			n, err := c.Read(buf)
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
		c.cost += cost
		c.count += 1
	}
	return nil
}

func TestClientEcho(clientnum, msgcount, msglen int, remoteAddr string, finish *sync.WaitGroup) {
	clients := make([]*client, clientnum)

	var wg sync.WaitGroup
	wg.Add(clientnum)

	for i := 0; i < clientnum; i++ {
		conn, err := net.Dial("tcp", remoteAddr)
		checkError(err)
		c := &client{
			TCPConn: conn.(*net.TCPConn),
		}
		clients[i] = c
		go func(j int) {
			time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
			echoTester(c, msglen, msgcount)
			wg.Done()
		}(i)
	}
	wg.Wait()

	var avgCostA float64
	echocount := 0
	for _, c := range clients {
		avgCostA += (c.cost / float64(c.count))
		echocount += c.count
		c.Close()
	}
	avgCost := avgCostA / float64(len(clients))

	fmt.Printf("TestClientEcho clientnum:%d msgcount:%v msglen:%v echocount:%v remoteAddr:%v avgCost:%v \n", clientnum, msgcount, msglen, echocount, remoteAddr, avgCost)

	finish.Done()
}

var clientnum = flag.Int("clientnum", 50, "input client number")
var msgcount = flag.Int("msgcount", 1000, "input msg count")
var msglen = flag.Int("msglen", 100, "input msg length")
var targetAddr = flag.String("targetAddr", "127.0.0.1:7900", "input target address")
var proxyAddr = flag.String("proxyAddr", "127.0.0.1:7890", "input proxy address")
var proxyAddrD = flag.String("proxyAddrD", "127.0.0.1:7891", "input proxy address direct")

func main() {
	flag.Parse()

	fmt.Printf("clientnum:%v\n", *clientnum)
	fmt.Printf("msgcount:%v\n", *msgcount)
	fmt.Printf("msglen:%v\n", *msglen)
	fmt.Printf("targetAddr:%v\n", *targetAddr)
	fmt.Printf("proxyAddr:%v\n", *proxyAddr)
	fmt.Printf("proxyAddrD:%v\n", *proxyAddrD)

	var finish sync.WaitGroup
	finish.Add(3)

	TestClientEcho(*clientnum, *msgcount, *msglen, *proxyAddr, &finish)
	TestClientEcho(*clientnum, *msgcount, *msglen, *proxyAddrD, &finish)
	TestClientEcho(*clientnum, *msgcount, *msglen, *targetAddr, &finish)

	finish.Wait()
}
