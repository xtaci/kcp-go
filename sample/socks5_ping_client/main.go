package main

import (
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ldcsoftware/kcp-go/sample/proxy"
)

func checkError(err error) {
	if err != nil {
		log.Println("checkError", err)
		os.Exit(-1)
	}
}

type client struct {
	*net.TCPConn
	idx     int
	proxy   string
	count   int
	cost    time.Duration
	avgCost time.Duration
	maxCost time.Duration

	countInterval   int
	costInterval    time.Duration
	maxCostInterval time.Duration

	lastOut time.Time
	err     error
}

func echoTester(c *client, msglen, msgcount, sendIntervalMs, outputIntervalS int, finish *sync.WaitGroup) (err error) {
	defer finish.Done()
	defer c.Close()
	defer func() {
		if c.count != 0 {
			c.avgCost = time.Duration(float64(c.cost) / float64(c.count))
		}
	}()

	buf := make([]byte, msglen)
	for {
		if msgcount != 0 && c.count >= msgcount {
			break
		}

		// send packet
		start := time.Now()
		if c.lastOut.IsZero() {
			c.lastOut = start
		}

		_, err = c.Write(buf)
		if err != nil {
			c.err = err
			log.Printf("client:%v proxy:%v write err:%v \n", c.idx, c.proxy, err)
			return err
		}

		// receive packet
		nrecv := 0
		var n int
		for {
			n, err = c.Read(buf)
			if err != nil {
				c.err = err
				log.Printf("client:%v proxy:%v read err:%v \n", c.idx, c.proxy, err)
				return err
			}
			nrecv += n
			if nrecv == msglen {
				break
			}
		}
		now := time.Now()
		cost := now.Sub(start)
		c.cost += cost
		if cost > c.maxCost {
			c.maxCost = cost
		}
		c.count += 1

		c.countInterval += 1
		c.costInterval += cost
		if cost > c.maxCostInterval {
			c.maxCostInterval = cost
		}

		if sendIntervalMs != 0 {
			time.Sleep(time.Duration(sendIntervalMs) * time.Millisecond)
		}

		if now.Sub(c.lastOut) > time.Second*time.Duration(outputIntervalS) {
			log.Printf("client:%v proxy:%v countTotal:%v avgCostTotal:%v maxCostTotal:%v countInterval:%v avgCostInterval:%v maxCostInterval:%v \n",
				c.idx, c.proxy, c.count, time.Duration(float64(c.cost)/float64(c.count)), c.maxCost, c.countInterval, time.Duration(float64(c.costInterval)/float64(c.countInterval)), c.maxCostInterval)
			c.lastOut = now

			c.countInterval = 0
			c.costInterval = 0
			c.maxCostInterval = 0
		}
	}
	return nil
}

var msgcount = flag.Int("msgcount", 0, "input msg count, zero means no limit")
var msglen = flag.Int("msglen", 1000, "input msg length")
var targetAddr = flag.String("targetAddr", "0.0.0.0:9259", "target address")
var proxyAddrs = flag.String("proxyAddrs", "", "proxy address")
var sendIntervalMs = flag.Int("sendIntervalMs", 200, "msg send interval millisecond")
var outputIntervalS = flag.Int("outputIntervalS", 1, "output interval second")

func main() {
	flag.Parse()

	log.Printf("msgcount:%v\n", *msgcount)
	log.Printf("msglen:%v\n", *msglen)
	log.Printf("targetAddr:%v\n", *targetAddr)
	log.Printf("proxyAddrs:%v\n", *proxyAddrs)
	log.Printf("sendIntervalMs:%v\n", *sendIntervalMs)
	log.Printf("outputIntervalS:%v\n", *outputIntervalS)

	proxies := strings.Split(*proxyAddrs, ",")
	clients := make([]*client, len(proxies))

	var finish sync.WaitGroup
	finish.Add(len(clients))

	for i := 0; i < len(clients); i++ {
		var conn net.Conn
		var err error
		var dialer proxy.Dialer
		if *proxyAddrs == "" {
			conn, err = net.Dial("tcp", *targetAddr)
			checkError(err)
		} else {
			dialer, err = proxy.SOCKS5("tcp", proxies[i], nil, &net.Dialer{})
			checkError(err)
			conn, err = dialer.Dial("tcp", *targetAddr)
			checkError(err)
		}
		c := &client{
			proxy:   proxies[i],
			TCPConn: conn.(*net.TCPConn),
			idx:     i + 1,
		}
		clients[i] = c
		go echoTester(c, *msglen, *msgcount, *sendIntervalMs, *outputIntervalS, &finish)
	}
	finish.Wait()

	for _, c := range clients {
		log.Printf("client:%v proxy:%v count:%v avgCost:%v maxCost:%v err:%v \n", c.idx, c.proxy, c.count, c.avgCost, c.maxCost, c.err)
	}
}
