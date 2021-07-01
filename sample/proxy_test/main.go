package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/ldcsoftware/kcp-go/sample/proxy"
)

const CostStatInterval = 100 * time.Millisecond
const CostStatMax = 3000 * time.Millisecond

var done uint32

var clients = flag.Int("clients", 1, "client count")
var msgcount = flag.Int("msgcount", 0, "input msg count, zero means no limit")
var msglen = flag.Int("msglen", 1024, "input msg length")
var targetAddr = flag.String("targetAddr", "0.0.0.0:9259", "target address")
var proxyAddr = flag.String("proxyAddr", "", "socks5 proxy address")
var directProxyAddr = flag.String("directProxyAddr", "", "direct proxy address")
var sendIntervalMs = flag.Int("sendIntervalMs", 1000, "msg send interval millisecond")
var outputIntervalS = flag.Int("outputIntervalS", 1, "output interval second")
var printRtoMs = flag.Int("printRtoMs", 1000, "print rto ms")

func checkError(err error) {
	if err != nil {
		log.Println("checkError", err)
		os.Exit(-1)
	}
}

/* decode 16 bits unsigned int (lsb) */
func ikcp_decode16u(p []byte, w *uint16) []byte {
	*w = binary.LittleEndian.Uint16(p)
	return p[2:]
}

type client struct {
	*net.TCPConn
	idx     int
	proxy   string
	count   int
	cost    time.Duration
	avgCost time.Duration
	maxCost time.Duration

	lastOut time.Time
	err     error
}

type CostStat struct {
	idx   int
	count int64
	cost  int64

	lastCount int64
	lastCost  int64
}

func (c *CostStat) Stat(cost int64) {
	atomic.AddInt64(&c.count, 1)
	atomic.AddInt64(&c.cost, cost)
}

func (c *CostStat) StepOutout() {
	count := atomic.LoadInt64(&c.count)
	cost := atomic.LoadInt64(&c.cost)

	stepCount := count - c.lastCount
	stepCost := cost - c.lastCost

	c.lastCount = count
	c.lastCost = cost

	if stepCount != 0 {
		avgCost := stepCost / stepCount
		log.Printf("cost stat. idx:%v count:%v avg:%v \n", c.idx, stepCount, time.Duration(avgCost))
	}
}

func echoTester(c *client, msglen, msgcount, sendIntervalMs int, costs []*CostStat) (err error) {
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
			n, err = c.Read(buf[nrecv:])
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

		idx := int(math.Floor(float64(cost) / float64(CostStatInterval)))
		if idx >= len(costs)-1 {
			idx = len(costs) - 1
		}
		costs[idx].Stat(int64(cost))

		if sendIntervalMs != 0 {
			time.Sleep(time.Duration(sendIntervalMs) * time.Millisecond)
		}
		printRtoDetail(c, cost, buf)
	}
	return nil
}

func printRtoDetail(c *client, cost time.Duration, buf []byte) {
	if cost < time.Millisecond*time.Duration((*printRtoMs)) {
		return
	}

	new := atomic.AddUint32(&done, 1)
	if new != 1 {
		return
	}

	var out, xmit_out, passts_out uint16
	ikcp_decode16u(buf[1000:], &out)
	ikcp_decode16u(buf[1002:], &xmit_out)
	ikcp_decode16u(buf[1004:], &passts_out)
	rtos_out := make([]uint16, 0, 500/2)
	for i := 0; i < 500; i += 2 {
		idx := uint16(i) + out
		if idx >= 500 {
			idx -= 500
		}
		var rto uint16
		ikcp_decode16u(buf[idx:], &rto)
		if rto > 0 {
			rtos_out = append(rtos_out, rto)
		}
	}
	log.Printf("client:%v cost:%v rtos_out:%v out:%v xmit_out:%v passts_out:%v \n",
		c.idx, cost, rtos_out, out, xmit_out, passts_out)

	var in, xmit_in, passts_in uint16
	ikcp_decode16u(buf[1010:], &in)
	ikcp_decode16u(buf[1012:], &xmit_in)
	ikcp_decode16u(buf[1014:], &passts_in)
	rtos_in := make([]uint16, 0, 500/2)
	for i := 0; i < 500; i += 2 {
		idx := uint16(i) + in
		if idx >= 500 {
			idx -= 500
		}
		idx += 500
		var rto uint16
		ikcp_decode16u(buf[idx:], &rto)
		if rto > 0 {
			rtos_in = append(rtos_in, rto)
		}
	}
	log.Printf("client:%v cost:%v rtos_in:%v in:%v xmit_in:%v passts_in:%v \n",
		c.idx, cost, rtos_in, in, xmit_in, passts_in)

	atomic.StoreUint32(&done, 0)
}

func main() {
	flag.Parse()

	log.Printf("clients:%v\n", *clients)
	log.Printf("msgcount:%v\n", *msgcount)
	log.Printf("msglen:%v\n", *msglen)
	log.Printf("targetAddr:%v\n", *targetAddr)
	log.Printf("proxyAddr:%v\n", *proxyAddr)
	log.Printf("directProxyAddr:%v\n", *directProxyAddr)
	log.Printf("sendIntervalMs:%v\n", *sendIntervalMs)
	log.Printf("outputIntervalS:%v\n", *outputIntervalS)
	log.Printf("printRtoMs:%v\n", *printRtoMs)

	clientcount := *clients

	clients := make([]*client, clientcount)
	batch := 100
	batchCount := (clientcount + batch - 1) / batch
	batchSleep := time.Millisecond * 100

	costs := make([]*CostStat, 0, CostStatMax/CostStatInterval)
	for i := 0; i < cap(costs); i++ {
		costs = append(costs, &CostStat{idx: i + 1})
	}

	fmt.Printf("TestClientEcho clientcount:%d batchCount:%v batchSleep:%v \n", clientcount, batchCount, batchSleep)

	for i := 0; i < batchCount; i++ {
		batchNum := batch
		if i == batchCount-1 {
			batchNum = clientcount - i*batch
		}

		fmt.Printf("TestClientEcho clientcount:%d batchIdx:%v batchNum:%v \n", clientcount, i, batchNum)

		for j := 0; j < batchNum; j++ {
			go func(batchIdx, idx int) {
				var conn net.Conn
				var err error
				var dialer proxy.Dialer
				var proxyAddrS string
				start := time.Now()
				if *proxyAddr != "" {
					dialer, err = proxy.SOCKS5("tcp", *proxyAddr, nil, &net.Dialer{})
					checkError(err)
					conn, err = dialer.Dial("tcp", *targetAddr)
					checkError(err)
					proxyAddrS = *proxyAddr
				} else if *directProxyAddr != "" {
					conn, err = net.Dial("tcp", *directProxyAddr)
					checkError(err)
					proxyAddrS = *directProxyAddr
				} else {
					conn, err = net.Dial("tcp", *targetAddr)
					checkError(err)
				}
				cost := time.Since(start)
				if cost > time.Millisecond*1000 {
					fmt.Printf("dial addr:%v cost:%v \n", proxyAddrS, cost)
				}
				c := &client{
					proxy:   proxyAddrS,
					TCPConn: conn.(*net.TCPConn),
					idx:     i + 1,
				}
				clients[batchIdx*batch+idx] = c
				echoTester(c, *msglen, *msgcount, *sendIntervalMs, costs)
			}(i, j)
		}
		time.Sleep(batchSleep)
	}

	for {
		for _, stat := range costs {
			stat.StepOutout()
		}
		fmt.Println("")
		time.Sleep(time.Second)
	}
}
