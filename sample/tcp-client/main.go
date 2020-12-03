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
	count   int
	cost    float64
	maxCost time.Duration
}

var clientnum = flag.Int("clientnum", 50, "input client number")
var msgcount = flag.Int("msgcount", 1000, "input msg count")
var msglen = flag.Int("msglen", 100, "input msg length")
var targetAddr = flag.String("targetAddr", "127.0.0.1:7900", "input target address")
var proxyAddr = flag.String("proxyAddr", "127.0.0.1:7890", "input proxy address")
var proxyAddrD = flag.String("proxyAddrD", "127.0.0.1:7891", "input proxy address direct")
var connectWay = flag.Int("connectWay", 0, "udp proxy, tcp proxy, direct")
var sendInterval = flag.Int("sendInterval", 0, "msg send interval millisecond")

func echoTester(c *client, msglen, msgcount int) (err error) {
	start := time.Now()
	fmt.Printf("echoTester start c:%v msglen:%v msgcount:%v start:%v\n", c.LocalAddr(), msglen, msgcount, start.Format("2006-01-02 15:04:05.000"))
	defer func() {
		fmt.Printf("echoTester end c:%v msglen:%v msgcount:%v count:%v cost:%v elasp:%v err:%v \n", c.LocalAddr(), msglen, msgcount, c.count, c.cost, time.Since(start), err)
	}()

	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		if *sendInterval != 0 {
			interval := rand.Intn(*sendInterval*2 + 1)
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}

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
		c.cost += cost
		if costTmp > c.maxCost {
			c.maxCost = costTmp
		}
		c.count += 1
	}
	return nil
}

func TestClientEcho(clientnum, msgcount, msglen int, remoteAddr string, finish *sync.WaitGroup) {
	baseSleep := time.Second * 2
	extraSleep := time.Duration(clientnum*3/2) * time.Millisecond

	batch := 100
	batchCount := (clientnum + batch - 1) / batch
	batchSleep := extraSleep / time.Duration(batchCount)

	clients := make([]*client, clientnum)

	fmt.Printf("TestClientEcho clientnum:%d batchCount:%v batchSleep:%v \n", clientnum, batchCount, batchSleep)

	var wg sync.WaitGroup
	wg.Add(clientnum)

	for i := 0; i < batchCount; i++ {
		batchNum := batch
		if i == batchCount-1 {
			batchNum = clientnum - i*batch
		}

		fmt.Printf("TestClientEcho clientnum:%d batchIdx:%v batchNum:%v \n", clientnum, i, batchNum)

		for j := 0; j < batchNum; j++ {
			go func(batchIdx, idx int) {
				conn, err := net.Dial("tcp", remoteAddr)
				checkError(err)
				c := &client{
					TCPConn: conn.(*net.TCPConn),
				}
				clients[batchIdx*batch+idx] = c
				time.Sleep(baseSleep + extraSleep - time.Duration(batchIdx)*batchSleep)
				echoTester(c, msglen, msgcount)
				wg.Done()
			}(i, j)
		}
		time.Sleep(batchSleep)
	}
	wg.Wait()

	var avgCostA float64
	var maxCost time.Duration
	echocount := 0
	for _, c := range clients {
		if c.count != 0 {
			avgCostA += (c.cost / float64(c.count))
			echocount += c.count
			if c.maxCost > maxCost {
				maxCost = c.maxCost
			}
		}
		c.Close()
	}
	avgCost := avgCostA / float64(len(clients))

	fmt.Printf("TestClientEcho clientnum:%d msgcount:%v msglen:%v echocount:%v remoteAddr:%v avgCost:%v maxCost:%v\n", clientnum, msgcount, msglen, echocount, remoteAddr, avgCost, maxCost)

	if finish != nil {
		finish.Done()
	}
}

func main() {
	flag.Parse()

	fmt.Printf("clientnum:%v\n", *clientnum)
	fmt.Printf("msgcount:%v\n", *msgcount)
	fmt.Printf("msglen:%v\n", *msglen)
	fmt.Printf("targetAddr:%v\n", *targetAddr)
	fmt.Printf("proxyAddr:%v\n", *proxyAddr)
	fmt.Printf("proxyAddrD:%v\n", *proxyAddrD)
	fmt.Printf("connectWay:%v\n", *connectWay)
	fmt.Printf("sendInterval:%v\n", *sendInterval)

	if *connectWay == 1 {
		TestClientEcho(*clientnum, *msgcount, *msglen, *proxyAddr, nil)
	} else if *connectWay == 2 {
		TestClientEcho(*clientnum, *msgcount, *msglen, *proxyAddrD, nil)
	} else if *connectWay == 3 {
		TestClientEcho(*clientnum, *msgcount, *msglen, *targetAddr, nil)
	} else {
		var finish sync.WaitGroup
		finish.Add(3)
		TestClientEcho(*clientnum, *msgcount, *msglen, *proxyAddr, &finish)
		time.Sleep(time.Second * 5)
		TestClientEcho(*clientnum, *msgcount, *msglen, *proxyAddrD, &finish)
		time.Sleep(time.Second * 5)
		TestClientEcho(*clientnum, *msgcount, *msglen, *targetAddr, &finish)
		finish.Wait()
	}
}
