package main

import (
	"bufio"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
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
		_, _, err := toUdpStreamBridge(s, conn)
		kcp.Logf(kcp.INFO, "toUDPStream stream:%v remote:%v err:%v", s.GetUUID(), conn.RemoteAddr(), err)
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

// handleClient aggregates connection p1 on mux with 'writeLock'
func handleClientEcho(s *kcp.UDPStream) {
	kcp.Logf(kcp.INFO, "handleClient start stream:%v remote:%v", s.GetUUID())
	defer kcp.Logf(kcp.INFO, "handleClient end stream:%v remote:%v", s.GetUUID())

	defer s.Close()

	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	for {
		n, err := s.Read(*buf)
		if n == 0 && err != nil {
			kcp.Logf(kcp.ERROR, "toTcpStreamBridge reading err:%v n:%v", err, n)
			break
		}

		testEchoInterval := int(binary.LittleEndian.Uint32(*buf))
		if testEchoInterval != 0 {
			interval := rand.Intn(testEchoInterval*2 + 1)
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}

		_, err = s.Write((*buf)[:n])
		if err != nil {
			kcp.Logf(kcp.ERROR, "toTcpStreamBridge writing err:%v", err)
			break
		}
	}
}

func iobridge(dst io.Writer, src io.Reader) {
	buf := bufPool.Get().(*[]byte)
	for {
		n, err := src.Read(*buf)
		if n == 0 && err != nil {
			log.Printf("iobridge reading err:%v n:%v \n", err, n)
			break
		}

		_, err = dst.Write((*buf)[:n])
		if err != nil {
			log.Printf("iobridge writing err:%v \n", err)
			break
		}
	}
	bufPool.Put(buf)

	log.Printf("iobridge end \n")
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

func hanldeFileTransfer(conn *kcp.UDPStream, size int) {
	defer conn.Close()

	file := randString(size)
	reader := strings.NewReader(file)

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

	log.Printf("hanldeFileTransfer finish \n")
	time.Sleep(time.Second * 5)
}

func main() {
	myApp := cli.NewApp()
	myApp.Name = "tun-client"
	myApp.Version = "1.0.0"
	myApp.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "cmdAddr",
			Value: "0.0.0.0:23000",
			Usage: "target address",
		},
		cli.StringFlag{
			Name:  "targetAddr",
			Value: "127.0.0.1:7900",
			Usage: "target address",
		},
		cli.StringFlag{
			Name:  "localIp",
			Value: "127.0.0.1",
			Usage: "local tunnel ip",
		},
		cli.StringFlag{
			Name:  "localPortS",
			Value: "8001",
			Usage: "local tunnel port start",
		},
		cli.StringFlag{
			Name:  "localPortE",
			Value: "8002",
			Usage: "local tunnel port end",
		},
		cli.StringFlag{
			Name:  "remoteIp",
			Value: "127.0.0.1",
			Usage: "remote tunnel ip",
		},
		cli.StringFlag{
			Name:  "remotePortS",
			Value: "7001",
			Usage: "remote tunnel port start",
		},
		cli.StringFlag{
			Name:  "remotePortE",
			Value: "7002",
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
			Name:  "parallelXmit",
			Value: 4,
			Usage: "parallel xmit",
		},
	}
	myApp.Action = func(c *cli.Context) error {
		testType := DefaultTest
		var testParam1 int64 = 0

		cmdAddr := c.String("cmdAddr")
		targetAddr := c.String("targetAddr")

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

		transmitTunsS := c.String("transmitTuns")
		transmitTuns, err := strconv.Atoi(transmitTunsS)
		checkError(err)

		logLevel := c.Int("logLevel")
		wndSize := c.Int("wndSize")
		bufferSize := c.Int("bufferSize")
		interval := c.Int("interval")
		inputQueueCount := c.Int("inputQueueCount")
		tunnelProcessorCount := c.Int("tunnelProcessorCount")
		noResend := c.Int("noResend")
		parallelXmit := c.Int("parallelXmit")

		fmt.Printf("Action cmdAddr:%v\n", cmdAddr)
		fmt.Printf("Action targetAddr:%v\n", targetAddr)
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
		fmt.Printf("Action parallelXmit:%v\n", parallelXmit)

		kcp.Logf = func(lvl kcp.LogLevel, f string, args ...interface{}) {
			if int(lvl) >= logLevel {
				logs[lvl].Printf(f+"\n", args...)
			}
		}

		opt := &kcp.TransportOption{
			AcceptBacklog:   1024,
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
			addr, err := net.ResolveTCPAddr("tcp", cmdAddr)
			checkError(err)

			cmdListener, err := net.ListenTCP("tcp", addr)
			checkError(err)

			for {
				conn, err := cmdListener.AcceptTCP()
				checkError(err)
				defer conn.Close()
				scanner := bufio.NewScanner(conn)
				scanner.Scan()
				log.Printf("recv cmd:%v \n", scanner.Text())
				sp := strings.SplitN(scanner.Text(), " ", 2)
				if len(sp) == 0 {
					log.Printf("unknown sp. sp:%v \n", scanner.Text())
					continue
				}
				cmd, err := strconv.ParseUint(sp[0], 10, 64)
				if err != nil || cmd < 0 || cmd >= CmdMax {
					log.Printf("unknown cmd. cmd:%v err:%v \n", cmd, err)
					continue
				}
				switch int(cmd) {
				case DefaultTest:
				case FileTransferTest:
					atomic.StoreInt64(&testParam1, 1024*1024*100)
					if len(sp) >= 2 {
						fileSize, err := strconv.ParseInt(sp[1], 10, 64)
						if err == nil {
							atomic.StoreInt64(&testParam1, fileSize)
						}
					}
				case StreamEchoTest:
				}
				testType = int(cmd)
				log.Printf("new test type sp:%v \n", sp)
			}
		}()

		for {
			stream, err := transport.Accept()
			checkError(err)
			stream.SetWindowSize(wndSize, wndSize*2)
			stream.SetNoDelay(kcp.FastStreamOption.Nodelay, interval, kcp.FastStreamOption.Resend, kcp.FastStreamOption.Nc)
			stream.SetParallelXmit(uint32(parallelXmit))
			go func() {
				switch testType {
				case DefaultTest:
					conn, err := net.Dial("tcp", targetAddr)
					if err != nil {
						kcp.Logf(kcp.ERROR, "open TCPStream failed. err:%v", err)
						return
					}
					handleClient(stream, conn.(*net.TCPConn))
				case FileTransferTest:
					hanldeFileTransfer(stream, int(atomic.LoadInt64(&testParam1)))
				case StreamEchoTest:
					handleClientEcho(stream)
				}
			}()
		}
		return nil
	}

	myApp.Run(os.Args)
}
