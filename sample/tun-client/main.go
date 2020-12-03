package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kcp "github.com/ldcsoftware/kcp-go"
	"github.com/urfave/cli"
)

const (
	DefaultTest = iota
	FileTransferTest
	StreamEchoTest
	CmdMax
)

var logs [5]*log.Logger

func init() {
	rand.Seed(time.Now().Unix())

	Debug := log.New(os.Stdout,
		"DEBUG: ",
		log.Ldate|log.Lmicroseconds)

	Info := log.New(os.Stdout,
		"INFO : ",
		log.Ldate|log.Lmicroseconds)

	Warning := log.New(os.Stdout,
		"WARN : ",
		log.Ldate|log.Lmicroseconds)

	Error := log.New(os.Stdout,
		"ERROR: ",
		log.Ldate|log.Lmicroseconds)

	Fatal := log.New(os.Stdout,
		"FATAL: ",
		log.Ldate|log.Lmicroseconds)

	logs = [int(kcp.FATAL) + 1]*log.Logger{Debug, Info, Warning, Error, Fatal}
}

var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func checkError(err error) {
	if err != nil {
		kcp.Logf(kcp.ERROR, "checkError: %+v\n", err)
		os.Exit(-1)
	}
}

func toUdpStreamBridge(dst *kcp.UDPStream, src *net.TCPConn) (wcount int, wcost float64, err error) {
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	for {
		start := 0
		n, err := src.Read(*buf)
		if n == 0 && err != nil {
			kcp.Logf(kcp.ERROR, "toUdpStreamBridge reading err:%v n:%v", err, n)
			return wcount, wcost, err
		}

		wstart := time.Now()
		_, err = dst.Write((*buf)[start:n])
		wcosttmp := time.Since(wstart)
		wcost += float64(wcosttmp.Nanoseconds()) / (1000 * 1000)
		wcount += 1
		if err != nil {
			kcp.Logf(kcp.ERROR, "toUdpStreamBridge writing err:%v", err)
			return wcount, wcost, err
		}
	}
	return wcount, wcost, nil
}

func toTcpStreamBridge(dst *net.TCPConn, src *kcp.UDPStream) (wcount int, wcost float64, err error) {
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	for {
		n, err := src.Read(*buf)
		if n == 0 && err != nil {
			kcp.Logf(kcp.ERROR, "toTcpStreamBridge reading err:%v n:%v", err, n)
			return wcount, wcost, err
		}

		wstart := time.Now()
		_, err = dst.Write((*buf)[:n])
		wcosttmp := time.Since(wstart)
		wcost += float64(wcosttmp.Nanoseconds()) / (1000 * 1000)
		wcount += 1
		if err != nil {
			kcp.Logf(kcp.ERROR, "toTcpStreamBridge writing err:%v", err)
			return wcount, wcost, err
		}
	}
	return wcount, wcost, nil
}

func iobridge(dst io.Writer, src io.Reader) (wcount int, wcost float64, err error) {
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	for {
		n, err := src.Read(*buf)
		if n == 0 && err != nil {
			kcp.Logf(kcp.ERROR, "iobridge reading err:%v n:%v", err, n)
			return wcount, wcost, err
		}

		wstart := time.Now()
		_, err = dst.Write((*buf)[:n])
		wcosttmp := time.Since(wstart)
		wcost += float64(wcosttmp.Nanoseconds()) / (1000 * 1000)
		wcount += 1
		if err != nil {
			kcp.Logf(kcp.ERROR, "iobridge writing err:%v", err)
			return wcount, wcost, err
		}
	}
	return wcount, wcost, nil
}

type TunnelPoll struct {
	tunnels []*kcp.UDPTunnel
	idx     uint32
}

func (poll *TunnelPoll) Add(tunnel *kcp.UDPTunnel) {
	poll.tunnels = append(poll.tunnels, tunnel)
}

func (poll *TunnelPoll) Pick() (tunnel *kcp.UDPTunnel) {
	idx := atomic.AddUint32(&poll.idx, 1) % uint32(len(poll.tunnels))
	return poll.tunnels[idx]
}

type TestSelector struct {
	remoteAddrs []net.Addr
	tunnels     []*kcp.UDPTunnel
	idx         uint32
}

func NewTestSelector() (*TestSelector, error) {
	return &TestSelector{
		tunnels: make([]*kcp.UDPTunnel, 0),
	}, nil
}

func (sel *TestSelector) Add(tunnel *kcp.UDPTunnel) {
	sel.tunnels = append(sel.tunnels, tunnel)
}

func (sel *TestSelector) Pick(remotes []string) (tunnels []*kcp.UDPTunnel) {
	for i := 0; i < len(remotes); i++ {
		idx := sel.idx % uint32(len(sel.tunnels))
		atomic.AddUint32(&sel.idx, 1)
		tunnels = append(tunnels, sel.tunnels[idx])
	}
	return tunnels
}

// handleClient aggregates connection p1 on mux with 'writeLock'
func handleClient(s *kcp.UDPStream, conn *net.TCPConn) {
	kcp.Logf(kcp.INFO, "handleClient start stream:%v remote:%v", s.GetUUID(), conn.RemoteAddr())
	defer kcp.Logf(kcp.INFO, "handleClient end stream:%v remote:%v", s.GetUUID(), conn.RemoteAddr())

	defer conn.Close()
	defer s.Close()

	shutdown := make(chan struct{}, 2)

	// start tunnel & wait for tunnel termination
	toUDPStream := func(s *kcp.UDPStream, conn *net.TCPConn, shutdown chan struct{}) {
		wcount, wcost, err := toUdpStreamBridge(s, conn)
		kcp.Logf(kcp.INFO, "toUDPStream stream:%v remote:%v wcount:%v wcost:%v err:%v", s.GetUUID(), conn.RemoteAddr(), wcount, wcost, err)
		shutdown <- struct{}{}
	}

	toTCPStream := func(conn *net.TCPConn, s *kcp.UDPStream, shutdown chan struct{}) {
		_, _, err := toTcpStreamBridge(conn, s)
		kcp.Logf(kcp.INFO, "toTCPStream stream:%v remote:%v err:%v", s.GetUUID(), conn.RemoteAddr(), err)
		shutdown <- struct{}{}
	}

	go toUDPStream(s, conn, shutdown)
	go toTCPStream(conn, s, shutdown)

	<-shutdown
}

// handleRawClient aggregates connection p1 on mux with 'writeLock'
func handleRawClient(targetConn *net.TCPConn, conn *net.TCPConn) {
	kcp.Logf(kcp.INFO, "handleRawClient start remote:%v", conn.RemoteAddr())
	defer kcp.Logf(kcp.INFO, "handleRawClient end remote:%v", conn.RemoteAddr())

	defer conn.Close()
	defer targetConn.Close()

	shutdown := make(chan struct{}, 2)

	// start tunnel & wait for tunnel termination
	shutdownIobridge := func(dst io.Writer, src io.Reader, shutdown chan struct{}) {
		iobridge(dst, src)
		shutdown <- struct{}{}
	}

	go shutdownIobridge(conn, targetConn, shutdown)
	shutdownIobridge(targetConn, conn, shutdown)

	<-shutdown
}

func main() {
	myApp := cli.NewApp()
	myApp.Name = "tun-client"
	myApp.Version = "1.0.0"
	myApp.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "listenAddr",
			Value: "127.0.0.1:7890",
			Usage: "local listen address",
		},
		cli.StringFlag{
			Name:  "localIp",
			Value: "127.0.0.1",
			Usage: "local tunnel ip",
		},
		cli.StringFlag{
			Name:  "localPortS",
			Value: "7001",
			Usage: "local tunnel port start",
		},
		cli.StringFlag{
			Name:  "localPortE",
			Value: "7002",
			Usage: "local tunnel port end",
		},
		cli.StringFlag{
			Name:  "remoteIp",
			Value: "127.0.0.1",
			Usage: "remote tunnel ip",
		},
		cli.StringFlag{
			Name:  "remotePortS",
			Value: "8001",
			Usage: "remote tunnel port start",
		},
		cli.StringFlag{
			Name:  "remotePortE",
			Value: "8002",
			Usage: "remote tunnel port end",
		},
		cli.StringFlag{
			Name:  "transmitTuns",
			Value: "2",
			Usage: "how many tunnels transmit data",
		},
		cli.StringFlag{
			Name:  "mode",
			Value: "fast",
			Usage: "profiles: fast3, fast2, fast, normal, manual",
		},
		cli.StringFlag{
			Name:  "listenAddrD",
			Value: "127.0.0.1:7891",
			Usage: "local listen address, direct connect target addr",
		},
		cli.StringFlag{
			Name:  "targetAddr",
			Value: "127.0.0.1:7900",
			Usage: "target address",
		},
		cli.StringFlag{
			Name:  "cmdAddr",
			Value: "0.0.0.0:23000",
			Usage: "cmd address",
		},
		cli.IntFlag{
			Name:  "logLevel",
			Value: 2,
			Usage: "kcp log level",
		},
		cli.IntFlag{
			Name:  "wndSize",
			Value: 128,
			Usage: "kcp send/recv wndSize",
		},
		cli.BoolFlag{
			Name:  "ackNoDelay",
			Usage: "stream ackNoDelay",
		},
		cli.IntFlag{
			Name:  "bufferSize",
			Value: 4 * 1024 * 1024,
			Usage: "kcp bufferSize",
		},
		cli.IntFlag{
			Name:  "interval",
			Value: 20,
			Usage: "kcp interval",
		},
		cli.IntFlag{
			Name:  "inputQueueCount",
			Value: 128,
			Usage: "transport inputQueueCount",
		},
		cli.IntFlag{
			Name:  "tunnelProcessorCount",
			Value: 10,
			Usage: "transport tunnelProcessorCount",
		},
		cli.IntFlag{
			Name:  "noResend",
			Value: 0,
			Usage: "kcp noResend",
		},
		cli.IntFlag{
			Name:  "timeout",
			Value: 0,
			Usage: "dial timeout",
		},
		cli.IntFlag{
			Name:  "parallelXmit",
			Value: 4,
			Usage: "parallel xmit",
		},
		cli.IntFlag{
			Name:  "testStreamCount",
			Value: 0,
			Usage: "test stream count",
		},
		cli.IntFlag{
			Name:  "testMsgCount",
			Value: 0,
			Usage: "self msg count",
		},
		cli.IntFlag{
			Name:  "testMsgLen",
			Value: 0,
			Usage: "test msg len",
		},
		cli.IntFlag{
			Name:  "testSendInterval",
			Value: 0,
			Usage: "test send interval",
		},
		cli.IntFlag{
			Name:  "testEchoInterval",
			Value: 0,
			Usage: "test echo interval",
		},
		cli.IntFlag{
			Name:  "testSendFileSize",
			Value: 0,
			Usage: "test send file size",
		},
		cli.IntFlag{
			Name:  "testRecvFileSize",
			Value: 0,
			Usage: "test recv file size",
		},
	}
	myApp.Action = func(c *cli.Context) error {
		listenAddr := c.String("listenAddr")
		listenAddrD := c.String("listenAddrD")
		targetAddr := c.String("targetAddr")
		cmdAddr := c.String("cmdAddr")

		localIp := c.String("localIp")
		lPortS := c.String("localPortS")
		localPortS, err := strconv.Atoi(lPortS)
		checkError(err)
		lPortE := c.String("localPortE")
		localPortE, err := strconv.Atoi(lPortE)
		checkError(err)

		remoteIp := c.String("remoteIp")
		rPortS := c.String("remotePortS")
		remotePortS, err := strconv.Atoi(rPortS)
		checkError(err)
		rPortE := c.String("remotePortE")
		remotePortE, err := strconv.Atoi(rPortE)
		checkError(err)

		logLevel := c.Int("logLevel")
		wndSize := c.Int("wndSize")
		bufferSize := c.Int("bufferSize")
		interval := c.Int("interval")
		inputQueueCount := c.Int("inputQueueCount")
		tunnelProcessorCount := c.Int("tunnelProcessorCount")
		noResend := c.Int("noResend")
		timeout := c.Int("timeout")
		parallelXmit := c.Int("parallelXmit")
		testStreamCount := c.Int("testStreamCount")
		testMsgCount := c.Int("testMsgCount")
		testMsgLen := c.Int("testMsgLen")
		testSendInterval := c.Int("testSendInterval")
		testEchoInterval := c.Int("testEchoInterval")
		testSendFileSize := c.Int("testSendFileSize")
		testRecvFileSize := c.Int("testRecvFileSize")

		transmitTunsS := c.String("transmitTuns")
		transmitTuns, err := strconv.Atoi(transmitTunsS)
		checkError(err)

		fmt.Printf("Action listenAddr:%v\n", listenAddr)
		fmt.Printf("Action listenAddrD:%v\n", listenAddrD)
		fmt.Printf("Action targetAddr:%v\n", targetAddr)
		fmt.Printf("Action cmdAddr:%v\n", cmdAddr)
		fmt.Printf("Action localIp:%v\n", localIp)
		fmt.Printf("Action localPortS:%v\n", localPortS)
		fmt.Printf("Action localPortE:%v\n", localPortE)
		fmt.Printf("Action remoteIp:%v\n", remoteIp)
		fmt.Printf("Action remotePortS:%v\n", remotePortS)
		fmt.Printf("Action remotePortE:%v\n", remotePortE)
		fmt.Printf("Action transmitTuns:%v\n", transmitTuns)
		fmt.Printf("Action logLevel:%v\n", logLevel)
		fmt.Printf("Action wndSize:%v\n", wndSize)
		fmt.Printf("Action bufferSize:%v\n", bufferSize)
		fmt.Printf("Action interval:%v\n", interval)
		fmt.Printf("Action inputQueueCount:%v\n", inputQueueCount)
		fmt.Printf("Action tunnelProcessorCount:%v\n", tunnelProcessorCount)
		fmt.Printf("Action noResend:%v\n", noResend)
		fmt.Printf("Action timeout:%v\n", timeout)
		fmt.Printf("Action parallelXmit:%v\n", parallelXmit)
		fmt.Printf("Action testStreamCount:%v\n", testStreamCount)
		fmt.Printf("Action testMsgCount:%v\n", testMsgCount)
		fmt.Printf("Action testMsgLen:%v\n", testMsgLen)
		fmt.Printf("Action testSendInterval:%v\n", testSendInterval)
		fmt.Printf("Action testEchoInterval:%v\n", testEchoInterval)
		fmt.Printf("Action testSendFileSize:%v\n", testSendFileSize)
		fmt.Printf("Action testRecvFileSize:%v\n", testRecvFileSize)

		kcp.Logf = func(lvl kcp.LogLevel, f string, args ...interface{}) {
			if int(lvl) >= logLevel {
				logs[lvl].Printf(f+"\n", args...)
			}
		}

		opt := &kcp.TransportOption{
			DialTimeout:     time.Minute,
			InputQueue:      inputQueueCount,
			TunnelProcessor: tunnelProcessorCount,
		}

		sel, err := NewTestSelector()
		checkError(err)
		transport, err := kcp.NewUDPTransport(sel, opt)
		checkError(err)
		for portS := localPortS; portS <= localPortE; portS++ {
			tunnel, err := transport.NewTunnel(localIp + ":" + strconv.Itoa(portS))
			checkError(err)
			err = tunnel.SetReadBuffer(bufferSize)
			checkError(err)
			err = tunnel.SetWriteBuffer(bufferSize)
			checkError(err)
		}

		if transmitTuns > (remotePortE - remotePortS + 1) {
			checkError(errors.New("invliad transmitTuns"))
		}

		locals := []string{}
		for portS := localPortS; portS <= localPortE; portS++ {
			locals = append(locals, localIp+":"+strconv.Itoa(portS))
		}

		remotes := []string{}
		for portS := remotePortS; portS <= remotePortE; portS++ {
			remotes = append(remotes, remoteIp+":"+strconv.Itoa(portS))
		}

		localIdx := 0
		remoteIdx := 0

		newUDPStream := func() (stream *kcp.UDPStream, err error) {
			tunLocals := []string{}
			for i := 0; i < transmitTuns; i++ {
				tunLocals = append(tunLocals, locals[localIdx%len(locals)])
				localIdx++
			}
			tunRemotes := []string{}
			for i := 0; i < transmitTuns; i++ {
				tunRemotes = append(tunRemotes, remotes[remoteIdx%len(remotes)])
				remoteIdx++
			}
			start := time.Now()
			stream, err = transport.OpenTimeout(tunLocals, tunRemotes, time.Duration(timeout)*time.Millisecond)
			if err != nil {
				kcp.Logf(kcp.ERROR, "OpenTimeout failed. err:%v \n", err)
				return nil, err
			}
			stream.SetWindowSize(wndSize, wndSize*2)
			stream.SetNoDelay(kcp.FastStreamOption.Nodelay, interval, kcp.FastStreamOption.Resend, kcp.FastStreamOption.Nc)
			stream.SetParallelXmit(uint32(parallelXmit))

			kcp.Logf(kcp.WARN, "Open UDPStream. uuid:%v cost:%v", stream.GetUUID(), time.Since(start))
			return stream, nil
		}

		setCmd := func(cmd int, params ...int) {
			cmdStr := fmt.Sprintf("%v", cmd)
			for i := 0; i < len(params); i++ {
				cmdStr += " "
				cmdStr += fmt.Sprintf("%v", params[i])
			}
			cmdStr += "\n"
			log.Printf("set cmd:%v", cmdStr)

			cmdConn, err := net.Dial("tcp", cmdAddr)
			if err != nil {
				log.Fatalf("set cmd err:%v", err)
				return
			}
			defer cmdConn.Close()
			n, err := cmdConn.Write([]byte(cmdStr))
			if n == 0 || err != nil {
				log.Fatalf("cmd write n:%v err:%v", n, err)
				return
			}
			time.Sleep(time.Second)
		}

		go func() {
			var intervalSeconds uint64 = 10
			var lastOutSegs uint64 = 0
			var lastInSegs uint64 = 0
			for {
				time.Sleep(time.Duration(intervalSeconds) * time.Second)
				OutSegs := atomic.LoadUint64(&kcp.DefaultSnmp.OutSegs)
				InSegs := atomic.LoadUint64(&kcp.DefaultSnmp.InSegs)
				outPerS := (OutSegs - lastOutSegs) / intervalSeconds
				inPerS := (InSegs - lastInSegs) / intervalSeconds
				lastOutSegs = OutSegs
				lastInSegs = InSegs
				headers := kcp.DefaultSnmp.Header()
				values := kcp.DefaultSnmp.ToSlice()
				fmt.Printf("------------- snmp result -------------\n")
				for i := 0; i < len(headers); i++ {
					fmt.Printf("snmp header:%v value:%v \n", headers[i], values[i])
				}
				fmt.Printf("snmp outSegs per seconds:%v \n", outPerS)
				fmt.Printf("snmp inSegs per seconds:%v \n", inPerS)
				fmt.Printf("------------- snmp result -------------\n\n")
			}
		}()

		go func() {
			addrD, err := net.ResolveTCPAddr("tcp", listenAddrD)
			checkError(err)
			listenerD, err := net.ListenTCP("tcp", addrD)
			checkError(err)

			for {
				conn, err := listenerD.AcceptTCP()
				checkError(err)
				go func() {
					start := time.Now()
					targetConn, err := net.Dial("tcp", targetAddr)
					if err != nil {
						kcp.Logf(kcp.ERROR, "Open TCPStream err:%v", err)
						return
					}
					kcp.Logf(kcp.WARN, "Open TCPStream. cost:%v", time.Since(start))
					handleRawClient(targetConn.(*net.TCPConn), conn)
				}()
			}
		}()

		go func() {
			addr, err := net.ResolveTCPAddr("tcp", listenAddr)
			checkError(err)
			listener, err := net.ListenTCP("tcp", addr)
			checkError(err)

			for {
				conn, err := listener.AcceptTCP()
				if err != nil {
					continue
				}
				go func() {
					stream, err := newUDPStream()
					if err != nil {
						return
					}
					handleClient(stream, conn)
				}()
			}
		}()

		if testStreamCount != 0 && testMsgCount != 0 && testMsgLen != 0 {
			testOpt := &streamEchoTestOption{
				streamCount:  testStreamCount,
				msgCount:     testMsgCount,
				msgLength:    testMsgLen,
				sendInterval: testSendInterval,
				echoInterval: testEchoInterval,
			}
			setCmd(StreamEchoTest)
			handleStreamTest(testOpt, newUDPStream)
		}

		if testSendFileSize != 0 && testRecvFileSize != 0 {
			testOpt := &fileTransferTestOption{
				size: testSendFileSize,
			}
			setCmd(FileTransferTest, testRecvFileSize)
			handleFileTransfer(testOpt, newUDPStream)
		}

		select {}
	}

	myApp.Run(os.Args)
}

type client struct {
	*kcp.UDPStream
	count   int
	cost    float64
	maxCost time.Duration
}

func echoTester(c *client, msglen, msgcount, sendInterval, echoInterval int) (err error) {
	start := time.Now()
	kcp.Logf(kcp.WARN, "echoTester start c:%v msglen:%v msgcount:%v start:%v\n", c.LocalAddr(), msglen, msgcount, start.Format("2006-01-02 15:04:05.000"))
	defer func() {
		kcp.Logf(kcp.WARN, "echoTester end c:%v msglen:%v msgcount:%v count:%v cost:%v elasp:%v err:%v \n", c.LocalAddr(), msglen, msgcount, c.count, c.cost, time.Since(start), err)
	}()

	buf := make([]byte, msglen)
	binary.LittleEndian.PutUint32(buf, uint32(echoInterval))
	for i := 0; i < msgcount; i++ {
		if sendInterval != 0 {
			interval := rand.Intn(sendInterval*2 + 1)
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
			if n == 0 && err != nil {
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

func TestClientEcho(clientnum, msgcount, msglen, sendInterval, echoInterval int, openStream func() (*kcp.UDPStream, error)) {
	baseSleep := time.Second * 2
	extraSleep := time.Duration(clientnum*3/2) * time.Millisecond

	batch := 100
	batchCount := (clientnum + batch - 1) / batch
	batchSleep := extraSleep / time.Duration(batchCount)

	clients := make([]*client, clientnum)

	kcp.Logf(kcp.WARN, "TestClientEcho clientnum:%v batchCount:%v batchSleep:%v", clientnum, batchCount, batchSleep)

	var wg sync.WaitGroup
	wg.Add(clientnum)

	for i := 0; i < batchCount; i++ {
		batchNum := batch
		if i == batchCount-1 {
			batchNum = clientnum - i*batch
		}

		kcp.Logf(kcp.WARN, "TestClientEcho clientnum:%v batchIdx:%v batchNum:%v", clientnum, i, batchNum)

		for j := 0; j < batchNum; j++ {
			go func(batchIdx, idx int) {
				defer wg.Done()
				stream, err := openStream()
				checkError(err)
				c := &client{
					UDPStream: stream,
				}
				clients[batchIdx*batch+idx] = c
				time.Sleep(baseSleep + extraSleep - time.Duration(batchIdx)*batchSleep)
				echoTester(c, msglen, msgcount, sendInterval, echoInterval)
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

	kcp.Logf(kcp.WARN, "TestClientEcho clientnum:%v msgcount:%v msglen:%v echocount:%v avgCost:%v maxCost:%v", clientnum, msgcount, msglen, echocount, avgCost, maxCost)
}

type streamEchoTestOption struct {
	streamCount  int
	msgCount     int
	msgLength    int
	sendInterval int
	echoInterval int
}

func handleStreamTest(opt *streamEchoTestOption, openStream func() (*kcp.UDPStream, error)) {
	TestClientEcho(opt.streamCount, opt.msgCount, opt.msgLength, opt.sendInterval, opt.echoInterval, openStream)
}

type CloseWriteConn interface {
	CloseWrite() error
}

type nwriter struct {
	n int
}

func (nw *nwriter) Write(b []byte) (n int, err error) {
	nw.n += len(b)
	return n, err
}

type fileToStream struct {
	src  io.ReadSeeker
	conn CloseWriteConn
	md5  string
	once bool
	hmd5 hash.Hash
}

type fileInfo struct {
	Md5  string `json:"md5"`
	Size int    `json:"size"`
}

func (fs *fileToStream) Read(b []byte) (n int, err error) {
	if !fs.once {
		fs.once = true

		fs.hmd5 = md5.New()
		nw := &nwriter{}
		mw := io.MultiWriter(fs.hmd5, nw)
		_, err := io.Copy(mw, fs.src)
		tempHash := fs.hmd5.Sum(nil)
		fs.md5 = base64.StdEncoding.EncodeToString(tempHash)
		fs.src.Seek(0, io.SeekStart)

		fi := fileInfo{
			Md5:  fs.md5,
			Size: nw.n,
		}
		fib, err := json.Marshal(fi)
		if err != nil {
			log.Fatal("file info marshal failed. err:%v", err)
		}

		binary.LittleEndian.PutUint32(b, uint32(len(fib)))
		n = copy(b[4:], fib)
		if len(fib) != n {
			log.Fatalf("fileToStream write md5 failed. err:%v", err)
		}
		n += 4

		log.Printf("fileToStream size:%v md5:%v n:%v", nw.n, fs.md5, n)
		return n, nil
	}
	n, err = fs.src.Read(b)
	if err == io.EOF {
		fs.conn.CloseWrite()
	}
	return n, err
}

type streamToFile struct {
	size     int
	md5      string
	once     bool
	recvSize int
	recvMd5  string
	hmd5     hash.Hash
}

func (sf *streamToFile) Write(b []byte) (n int, err error) {
	if !sf.once {
		sf.once = true

		if len(b) < 4 {
			log.Fatalf("b less than 4")
		}
		filen := int(binary.LittleEndian.Uint32(b))
		if len(b) < 4+filen {
			log.Fatalf("b less then need:%v got:%v", 4+filen, len(b))
		}

		fi := fileInfo{}
		err := json.Unmarshal(b[4:4+filen], &fi)
		if err != nil {
			log.Fatal("file info unmarshal failed. err:%v", err)
		}
		sf.recvMd5 = fi.Md5
		sf.recvSize = fi.Size
		sf.hmd5 = md5.New()
		n = filen + 4

		log.Printf("streamToFile recvSize:%v recvMd5:%v n:%v left:%v \n", fi.Size, fi.Md5, n, len(b)-n)
		if n >= len(b) {
			return n, nil
		} else {
			b = b[n:]
		}
	}
	if len(sf.md5) != 0 {
		log.Fatalf("file recv wrong, md5 alreaddy calc. n:%v \n", len(b))
	}

	sf.size += len(b)
	nn, err := sf.hmd5.Write(b)
	n += nn
	if sf.size >= sf.recvSize {
		sf.md5 = base64.StdEncoding.EncodeToString(sf.hmd5.Sum(nil))
	}
	return n, err
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

type fileTransferTestOption struct {
	size int
}

func handleFileTransfer(opt *fileTransferTestOption, openStream func() (*kcp.UDPStream, error)) {
	file := randString(opt.size)
	reader := strings.NewReader(file)

	conn, err := openStream()
	if err != nil {
		fmt.Println("Error connecting:", err)
		os.Exit(1)
	}
	defer conn.Close()

	shutdown := make(chan bool, 2)

	//send file
	go func() {
		fs := &fileToStream{src: reader, conn: conn}
		iobridge(conn, fs)
		log.Printf("handleFileTransfer file send finish. md5:%v \n", fs.md5)

		shutdown <- true
	}()

	//recv file
	go func() {
		sf := &streamToFile{}
		iobridge(sf, conn)
		log.Printf("handleFileTransfer file recv finish. recvSize:%v recvMd5:%v size:%v md5:%v equal:%v \n",
			sf.recvSize, sf.recvMd5, sf.size, sf.md5, sf.recvMd5 == sf.md5)

		shutdown <- true
	}()

	<-shutdown
	<-shutdown

	log.Printf("handleFileTransfer finish \n")
	time.Sleep(time.Second * 5)
}
