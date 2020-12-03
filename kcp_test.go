package kcp

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
)

var logs [5]*log.Logger

type TestSelector struct {
	locals  []string
	remotes []string
	tunnels []*UDPTunnel
}

func NewTestSelector(locals, remotes []string) (*TestSelector, error) {
	return &TestSelector{
		tunnels: make([]*UDPTunnel, 0),
		locals:  locals,
		remotes: remotes,
	}, nil
}

func (sel *TestSelector) Add(tunnel *UDPTunnel) {
	sel.tunnels = append(sel.tunnels, tunnel)
}

func (sel *TestSelector) PickAddrs(count int) (locals, remotes []string) {
	return sel.locals[:count], sel.remotes[:count]
}

func (sel *TestSelector) Pick(remotes []string) (tunnels []*UDPTunnel) {
	return sel.tunnels[:len(remotes)]
}

var lPortStart = 7001
var lPortCount = 2
var rPortStart = 17001
var rPortCount = 2

var lAddrs = []string{}
var rAddrs = []string{}
var remoteIps = []string{}
var ipsCount = 2

// var lAddrs = []string{"127.0.0.1:17001"}
// var rAddrs = []string{"127.0.0.1:18001"}
// var remoteIps = []string{"127.0.0.1"}

var clientSel *TestSelector
var clientTransport *UDPTransport
var clientTunnels []*UDPTunnel
var serverTransport *UDPTransport
var serverTunnels []*UDPTunnel

func InitLog(l LogLevel) {
	Logf = func(lvl LogLevel, f string, args ...interface{}) {
		if lvl >= l {
			logs[lvl].Printf(f+"\n", args...)
		}
	}
}

func Init(l LogLevel) {
	InitLog(l)

	for i := 0; i < lPortCount; i++ {
		lAddrs = append(lAddrs, "127.0.0.1:"+strconv.Itoa(lPortStart+i))
	}
	for i := 0; i < rPortCount; i++ {
		rAddrs = append(rAddrs, "127.0.0.1:"+strconv.Itoa(rPortStart+i))
	}
	for i := 0; i < ipsCount; i++ {
		remoteIps = append(remoteIps, "127.0.0.1")
	}

	Logf(INFO, "init")

	topt := &TransportOption{
		DialTimeout:          time.Second * 2,
		ParallelCheckPeriods: 2,
		ParallelStreamRate:   0.1,
		ParallelDuration:     time.Second * 60,
	}

	var err error
	clientSel, err = NewTestSelector(lAddrs, rAddrs)
	if err != nil {
		panic("NewTestSelector")
	}
	clientTransport, err = NewUDPTransport(clientSel, topt)
	if err != nil {
		panic("NewUDPTransport")
	}
	for _, lAddr := range lAddrs {
		tunnel, err := clientTransport.NewTunnel(lAddr)
		if err != nil {
			panic("NewTunnel" + err.Error())
		}
		clientTunnels = append(clientTunnels, tunnel)
	}

	serverSel, err := NewTestSelector(rAddrs, lAddrs)
	if err != nil {
		panic("NewTestSelector")
	}
	serverTransport, err = NewUDPTransport(serverSel, topt)
	if err != nil {
		panic("NewUDPTransport")
	}
	for _, rAddr := range rAddrs {
		tunnel, err := serverTransport.NewTunnel(rAddr)
		if err != nil {
			panic("NewTunnel")
		}
		serverTunnels = append(serverTunnels, tunnel)
	}
}

func init() {
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

	logs = [int(FATAL) + 1]*log.Logger{Debug, Info, Warning, Error, Fatal}

	Init(DEBUG)
}

func setTunnelBuffer(sendBuffer, recvBuffer int) error {
	tunnels := append(serverTunnels, clientTunnels...)
	for _, tunnel := range tunnels {
		err := tunnel.SetReadBuffer(4 * 1024 * 1024)
		if err != nil {
			return err
		}
		err = tunnel.SetWriteBuffer(4 * 1024 * 1024)
		if err != nil {
			return err
		}
	}
	return nil
}

const repeat = 16

func checkError(t *testing.T, err error) {
	if err != nil {
		Logf(ERROR, "checkError: %+v\n", err)
		t.Fatal(err)
	}
}

func TestTransportOption(t *testing.T) {
	opt := &TransportOption{
		DialTimeout: time.Second,
		InputQueue:  100,
	}
	opt.SetDefault()

	if opt.AcceptBacklog != DefaultAcceptBacklog {
		t.Fatal("AcceptBacklog")
	}
	if opt.DialTimeout != time.Second {
		t.Fatal("DialTimeout")
	}
	if opt.InputQueue != 100 {
		t.Fatal("InputQueue")
	}
	if opt.TunnelProcessor != DefaultTunnelProcessor {
		t.Fatal("TunnelProcessor")
	}
	if opt.InputTime != DefaultInputTime {
		t.Fatal("InputTime")
	}
}

func tunnelSimulate(tunnels []*UDPTunnel, loss float64, delayMin, delayMax int) {
	for _, tunnel := range tunnels {
		tunnel.Simulate(loss, delayMin, delayMax)
	}
}

func server(t *testing.T, nodelay, interval, resend, nc int) {
	stream, err := serverTransport.Accept()
	if err != nil {
		t.Fatalf("server accept stream failed. err:%v", err)
	}
	defer stream.Close()
	stream.SetNoDelay(nodelay, interval, resend, nc)

	buf := make([]byte, 65536)
	for {
		n, err := stream.Read(buf)
		if n == 0 && err != nil {
			return
		}
		stream.Write(buf[:n])
	}
}

func client(t *testing.T, nodelay, interval, resend, nc int) {
	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()
	stream.SetNoDelay(nodelay, interval, resend, nc)

	buf := make([]byte, 64)
	var rtt time.Duration
	for i := 0; i < repeat; i++ {
		start := time.Now()
		stream.Write(buf)
		io.ReadFull(stream, buf)
		rtt += time.Now().Sub(start)
	}

	Logf(INFO, "avg rtt:%v", rtt/repeat)
	Logf(INFO, "total time: %v for %v round trip:", rtt, repeat)
}

func handleEchoClient(stream *UDPStream) {
	defer stream.Close()

	stream.SetNoDelay(1, 10, 2, 1)
	stream.SetWindowSize(4096, 4096)
	stream.SetNoDelay(1, 10, 2, 1)
	stream.SetMtu(1400)
	stream.SetACKNoDelay(false)
	stream.SetReadDeadline(time.Now().Add(time.Hour))
	stream.SetWriteDeadline(time.Now().Add(time.Hour))

	buf := make([]byte, 65536)
	for {
		n, err := stream.Read(buf)
		if n == 0 && err != nil {
			return
		}
		stream.Write(buf[:n])
	}
}

func handleSinkClient(stream *UDPStream) {
	defer stream.Close()

	stream.SetNoDelay(1, 10, 2, 1)
	stream.SetWindowSize(4096, 4096)
	stream.SetNoDelay(1, 10, 2, 1)
	stream.SetMtu(1400)
	stream.SetACKNoDelay(false)
	stream.SetReadDeadline(time.Now().Add(time.Hour))
	stream.SetWriteDeadline(time.Now().Add(time.Hour))

	buf := make([]byte, 65536)
	for {
		_, err := stream.Read(buf)
		if err != nil {
			return
		}
	}
}

func echoServer() {
	stream, err := serverTransport.Accept()
	if err != nil {
		Logf(ERROR, "echoServer accept err:%v", err)
	}
	handleEchoClient(stream)
}

func sinkServer() {
	stream, err := serverTransport.Accept()
	if err != nil {
		Logf(ERROR, "sinkServer accept err:%v", err)
	}
	handleSinkClient(stream)
}

func echoTester(stream *UDPStream, msglen, msgcount int) error {
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		// send packet
		_, err := stream.Write(buf)
		if err != nil {
			return err
		}

		// receive packet
		nrecv := 0
		for {
			n, err := stream.Read(buf)
			if n == 0 && err != nil {
				return err
			}
			nrecv += n
			if nrecv == msglen {
				break
			}
		}
	}
	return nil
}

func sinkTester(stream *UDPStream, msglen, msgcount int) error {
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		// send packet
		_, err := stream.Write(buf)
		if err != nil {
			return err
		}
	}
	return nil
}

func echoClient(nbytes, N int) error {
	locals, remotes := clientSel.PickAddrs(ipsCount)
	stream, err := clientTransport.OpenTimeout(locals, remotes, time.Second*10)
	if err != nil {
		return err
	}
	defer stream.Close()
	stream.SetWindowSize(1024, 1024)
	stream.SetNoDelay(1, 10, 2, 1)
	stream.SetMtu(1400)
	stream.SetMtu(1600)
	stream.SetMtu(1400)
	stream.SetACKNoDelay(true)
	stream.SetACKNoDelay(false)
	stream.SetDeadline(time.Now().Add(time.Hour))

	err = echoTester(stream, nbytes, N)
	if err != nil {
		return err
	}
	return nil
}

func sinkClient(nbytes, N int) error {
	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		return err
	}
	defer stream.Close()
	stream.SetWindowSize(1024, 1024)
	stream.SetNoDelay(1, 10, 2, 1)
	stream.SetMtu(1400)
	stream.SetMtu(1600)
	stream.SetMtu(1400)
	stream.SetACKNoDelay(true)
	stream.SetACKNoDelay(false)
	stream.SetDeadline(time.Now().Add(time.Hour))

	err = sinkTester(stream, nbytes, N)
	if err != nil {
		return err
	}
	return nil
}

func testlink(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	Logf(INFO, "testing with nodelay parameters, loss:%v delayMin:%v delayMax:%v nodelay:%v interval:%v resend:%v nc:%v",
		loss, delayMin, delayMax, nodelay, interval, resend, nc)

	tunnelSimulate(clientTunnels, loss, delayMin, delayMax)
	tunnelSimulate(serverTunnels, loss, delayMin, delayMax)

	go server(t, nodelay, interval, resend, nc)
	client(t, nodelay, interval, resend, nc)

	tunnelSimulate(clientTunnels, 0, 0, 0)
	tunnelSimulate(serverTunnels, 0, 0, 0)
}

func testLossyConn1(t *testing.T) {
	Logf(INFO, "testing loss rate 0.1, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 1")
	testlink(t, 0.1, 10, 20, 1, 10, 2, 1)
}

func testLossyConn2(t *testing.T) {
	Logf(INFO, "testing loss rate 0.2, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 1")
	testlink(t, 0.2, 10, 20, 1, 10, 2, 1)
}

func testLossyConn3(t *testing.T) {
	Logf(INFO, "testing loss rate 0.3, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 1")
	testlink(t, 0.3, 10, 20, 1, 10, 2, 1)
}

func testLossyConn4(t *testing.T) {
	Logf(INFO, "testing loss rate 0.1, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 0")
	testlink(t, 0.1, 10, 20, 1, 10, 2, 0)
}

func TestLossyConn(t *testing.T) {
	Logf(INFO, "TestKCP.testLossyConn1")
	testLossyConn1(t)

	Logf(INFO, "TestKCP.testLossyConn2")
	testLossyConn2(t)

	Logf(INFO, "TestKCP.testLossyConn3")
	testLossyConn3(t)

	Logf(INFO, "TestKCP.testLossyConn4")
	testLossyConn4(t)
}

func TestLanBroken(t *testing.T) {

	go func() {
		stream, err := serverTransport.Accept()
		if err != nil {
			Logf(ERROR, "echoServer accept err:%v", err)
		}
		tunnel := stream.tunnels[0]
		tunnel.Simulate(100, 0, 0)
		handleEchoClient(stream)
		tunnel.Simulate(0, 0, 0)
	}()

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()
	tunnel := stream.tunnels[0]
	tunnel.Simulate(100, 0, 0)

	const N = 100
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		stream.Write([]byte(msg))
		if n, err := stream.Read(buf); n != 0 {
			if string(buf[:n]) != msg {
				t.Fatal("TestSendRecv msg not equal", err)
			}
		} else {
			t.Fatal("TestSendRecv", err)
		}
	}
	tunnel.Simulate(0, 0, 0)
}

func TestTimeout(t *testing.T) {
	go echoServer()

	buf := make([]byte, 10)
	//timeout
	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()

	stream.SetDeadline(time.Now().Add(time.Second))
	<-time.After(2 * time.Second)
	n, err := stream.Read(buf)
	if n != 0 || err == nil {
		t.Fail()
	}
	Logf(INFO, "TestTimeout err:%v", err)
}

func TestSendRecv(t *testing.T) {
	go echoServer()

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()

	const N = 100
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		stream.Write([]byte(msg))
		if n, err := stream.Read(buf); n != 0 {
			if string(buf[:n]) != msg {
				t.Fatal("TestSendRecv msg not equal", err)
			}
		} else {
			t.Fatal("TestSendRecv", err)
		}
	}
}

func tinyRecvServer(t *testing.T) {
	stream, err := serverTransport.Accept()
	if err != nil {
		Logf(ERROR, "echoServer accept err:%v", err)
	}
	defer stream.Close()

	buf := make([]byte, 2)
	for {
		n, err := stream.Read(buf)
		if n == 0 && err != nil {
			return
		}
		stream.Write(buf[:n])
	}
}

func TestTinyBufferReceiver(t *testing.T) {
	go tinyRecvServer(t)

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()

	const N = 100
	snd := byte(0)
	fillBuffer := func(buf []byte) {
		for i := 0; i < len(buf); i++ {
			buf[i] = snd
			snd++
		}
	}

	rcv := byte(0)
	check := func(buf []byte) bool {
		for i := 0; i < len(buf); i++ {
			if buf[i] != rcv {
				return false
			}
			rcv++
		}
		return true
	}
	sndbuf := make([]byte, 7)
	rcvbuf := make([]byte, 7)
	for i := 0; i < N; i++ {
		fillBuffer(sndbuf)
		stream.Write(sndbuf)
		if n, err := io.ReadFull(stream, rcvbuf); err == nil {
			if !check(rcvbuf[:n]) {
				t.Fail()
			}
		} else {
			checkError(t, err)
		}
	}
}

func randomSendServer(t *testing.T, size int, sizeMax int) {
	stream, err := serverTransport.Accept()
	if err != nil {
		Logf(ERROR, "echoServer accept err:%v", err)
	}
	defer stream.Close()

	buf := make([]byte, size)
	for i := 0; i < len(buf); i++ {
		buf[i] = byte((i % 256))
	}

	sendSizeMax := sizeMax
	totalSendSize := 0

	for {
		sendSize := rand.Intn(sendSizeMax)
		if totalSendSize+sendSize > size {
			sendSize = size - totalSendSize
		}
		stream.Write(buf[totalSendSize : totalSendSize+sendSize])
		totalSendSize += sendSize
		if totalSendSize >= size {
			break
		}
	}
}

func TestRandomBufferReceiver(t *testing.T) {
	size := 1024 * 1024
	sizeMax := 1024 * 128
	go randomSendServer(t, size, sizeMax)

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()

	rcevSizeMax := sizeMax
	totalRecvSize := 0
	buf := make([]byte, size)
	maxN := 0
	for {
		recvSize := rand.Intn(rcevSizeMax)
		if totalRecvSize+recvSize > size {
			recvSize = size - totalRecvSize
		}
		n, err := stream.Read(buf[totalRecvSize : totalRecvSize+recvSize])
		recvSize = n
		if n > maxN {
			maxN = n
		}
		if err != nil {
			t.Fatalf("read size wrong or err is not nil. n:%v recvSize:%v err:%v", n, recvSize, err)
		}
		for i := totalRecvSize; i < totalRecvSize+recvSize; i++ {
			if buf[i] != byte(i%256) {
				t.Fatalf("random buf read faild. i:%v value:%v target:%v", i, buf[i], byte(i%256))
			}
		}
		totalRecvSize += recvSize
		if totalRecvSize >= size {
			break
		}
	}
	n, err := stream.Read(buf)
	if n != 0 {
		t.Fatalf("read size wrong or err is not eof. n:%v err:%v", n, err)
	}
	log.Printf("maxN:%v", maxN)
}

func TestClose(t *testing.T) {
	go echoServer()

	var n int
	var err error

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("client open stream failed. err:%v", err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(time.Second))

	buf := make([]byte, 10)
	n, err = stream.Write(buf)
	if n == 0 || err != nil {
		t.Fatalf("Write misbehavior. err:%v", err)
	}

	stream.CloseWrite()
	n, err = stream.Read(buf[:5])
	if n == 0 {
		t.Fatalf("Read after CloseWrite misbehavior. n:%v err:%v", n, err)
	}

	n, err = stream.Write(buf)
	if n != 0 || err != io.ErrClosedPipe {
		t.Fatal("Write after CloseWrite misbehavior")
	}

	err = stream.Close()
	n, err = stream.Read(buf)
	if n != 0 || err != io.ErrClosedPipe {
		t.Fatal("Read after Close misbehavior")
	}

	err = stream.Close()
	if err != io.ErrClosedPipe {
		t.Fatal("Close after Close misbehavior")
	}
}

func TestSNMP(t *testing.T) {
	Logf(INFO, "DefaultSnmp.Copy:%v", DefaultSnmp.Copy())
	Logf(INFO, "DefaultSnmp.Header:%v", DefaultSnmp.Header())
	Logf(INFO, "DefaultSnmp.ToSlice:%v", DefaultSnmp.ToSlice())
	Logf(INFO, "DefaultSnmp.ToSlice:%v", DefaultSnmp.ToSlice())

	currEstab := DefaultSnmp.CurrEstab
	Logf(INFO, "start establish:%v", DefaultSnmp.CurrEstab)

	locals := []string{"127.0.0.1:10001"}
	remotes := []string{"127.0.0.1:10002"}
	_, err := clientTransport.Open(locals, remotes)
	if err == nil {
		t.Fatalf("test snmp failed")
	}

	Logf(INFO, "open invalid establish:%v", DefaultSnmp.CurrEstab)
	if currEstab != DefaultSnmp.CurrEstab {
		t.Fatalf("test snmp failed")
	}

	go echoServer()

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("test snmp failed")
	}

	Logf(INFO, "connect establish:%v", DefaultSnmp.CurrEstab)
	if currEstab+2 != DefaultSnmp.CurrEstab {
		t.Fatalf("test snmp failed")
	}

	stream.Close()
	time.Sleep(time.Millisecond * 500)

	Logf(INFO, "after close establish:%v", DefaultSnmp.CurrEstab)
	if currEstab != DefaultSnmp.CurrEstab {
		t.Fatalf("test snmp failed")
	}
}

func TestParallel1024CLIENT_64BMSG_64CNT(t *testing.T) {
	var wg sync.WaitGroup
	N := 200
	wg.Add(N)
	for i := 0; i < N; i++ {
		go echoServer()
	}

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			err := echoClient(64, 64)
			if err != nil {
				t.Fatal("echoClient", err)
			}
		}()
	}
	wg.Wait()
}

func TestAcceptBackuplog(t *testing.T) {
	serverTransport.preAcceptChan = make(chan chan *UDPStream, 30)
	var wg sync.WaitGroup
	N := 500
	wg.Add(N)
	for i := 0; i < N; i++ {
		go echoServer()
	}

	for i := 0; i < N; i++ {
		go func() {
			locals, remotes := clientSel.PickAddrs(ipsCount)
			stream, err := clientTransport.OpenTimeout(locals, remotes, time.Second*10)
			if err != nil {
				t.Fatal("Open failed", err)
			}
			defer stream.Close()
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestGlobalParallel(t *testing.T) {
	hpc := clientTransport.pc.getHostParallel("127.0.0.1")
	hpc.reset()

	hps := serverTransport.pc.getHostParallel("127.0.0.1")
	hps.reset()

	clientTunnels[0].Simulate(100, 0, 0)
	serverTunnels[0].Simulate(100, 0, 0)

	var wg sync.WaitGroup
	N := 200
	wg.Add(N)
	for i := 0; i < N; i++ {
		go echoServer()
	}

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			err := echoClient(64, 64)
			if err != nil {
				t.Fatal("echoClient", err)
			}
		}()
	}
	wg.Wait()

	if !hpc.isParallel() {
		t.Fatalf("does not enter global parallel")
	}
}

func echoSpeed(b *testing.B, bytes int) {
	err := setTunnelBuffer(4*1024*1024, 4*1024*1024)
	if err != nil {
		b.Fatal("echoSpeed setTunnelBuffer", err)
	}
	b.ReportAllocs()

	go echoServer()
	echoClient(bytes, b.N)
	b.SetBytes(int64(bytes))
}

func sinkSpeed(b *testing.B, bytes int) {
	err := setTunnelBuffer(4*1024*1024, 4*1024*1024)
	if err != nil {
		b.Fatal("echoSpeed setTunnelBuffer", err)
	}
	b.ReportAllocs()

	go sinkServer()
	sinkClient(bytes, b.N)
	b.SetBytes(int64(bytes))
}

var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func iobridge(src io.Reader, dst io.Writer) {
	buf := bufPool.Get().(*[]byte)
	for {
		n, err := src.Read(*buf)
		if n == 0 && err != nil {
			Logf(INFO, "iobridge reading err:%v n:%v", err, n)
			break
		}

		_, err = dst.Write((*buf)[:n])
		if err != nil {
			Logf(INFO, "iobridge writing err:%v", err)
			break
		}
	}
	bufPool.Put(buf)

	Logf(INFO, "iobridge end")
}

type fileToStream struct {
	src  io.Reader
	conn net.Conn
}

func (fs *fileToStream) Read(b []byte) (n int, err error) {
	n, err = fs.src.Read(b)
	if err == io.EOF {
		if stream, ok := fs.conn.(*UDPStream); ok {
			stream.CloseWrite()
		} else if tcpconn, ok := fs.conn.(*net.TCPConn); ok {
			tcpconn.CloseWrite()
		}
	}
	return n, err
}

func FileTransfer(t *testing.T, conn net.Conn, lf io.Reader, rFileHash []byte) error {
	Logf(INFO, "FileTransfer start")

	// bridge connection
	shutdown := make(chan bool, 2)

	go func() {
		fs := &fileToStream{lf, conn}
		iobridge(fs, conn)
		shutdown <- true

		Logf(INFO, "FileTransfer file send finish. not hash:%v", rFileHash)
	}()

	go func() {
		h := md5.New()
		iobridge(conn, h)
		recvHash := h.Sum(nil)
		if !bytes.Equal(rFileHash, recvHash) {
			t.Fatalf("FileTransfer recv hash not equal. fileHash:%v recvHash:%v", rFileHash, recvHash)
		}
		shutdown <- true

		Logf(INFO, "FileTransfer file recv finish. fileHash:%v", rFileHash)
	}()

	<-shutdown
	<-shutdown
	return nil
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

func TestTCPFileTransfer(t *testing.T) {
	lFile := randString(1024 * 512)
	lReader := strings.NewReader(lFile)
	rFile := randString(1024 * 1024 * 16)
	rReader := strings.NewReader(rFile)

	lh := md5.New()
	_, err := io.Copy(lh, lReader)
	lFileHash := lh.Sum(nil)
	Logf(INFO, "lFileLen:%v hash:%v err:%v", len(lFile), lFileHash, err)

	rh := md5.New()
	_, err = io.Copy(rh, rReader)
	rFileHash := rh.Sum(nil)
	Logf(INFO, "rFileLen:%v hash:%v err:%v", len(rFile), rFileHash, err)

	lReader.Seek(0, io.SeekStart)
	rReader.Seek(0, io.SeekStart)

	l, err := net.Listen("tcp", "100.100.35.71:")
	if err != nil {
		t.Fatalf("Listen failed. err: %v", err)
	}

	var serverConn net.Conn
	var clientConn net.Conn

	go func() {
		clientConn, err = net.Dial("tcp", l.Addr().String())
		if err != nil {
			fmt.Println("Error connecting:", err)
			os.Exit(1)
		}
	}()

	go func() {
		serverConn, err = l.Accept()
		if err != nil {
			t.Fatalf("FileTransferServer Accept stream %v", err)
		}
	}()

	for {
		if serverConn != nil && clientConn != nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	finish := make(chan struct{}, 2)
	go func() {
		FileTransfer(t, serverConn, rReader, lFileHash)
		finish <- struct{}{}
	}()

	go func() {
		FileTransfer(t, clientConn, lReader, rFileHash)
		finish <- struct{}{}
	}()

	<-finish
	<-finish
}

func TestUDPFileTransfer(t *testing.T) {
	lFile := randString(1024)
	lReader := strings.NewReader(lFile)
	rFile := randString(1024 * 1024 * 16)
	rReader := strings.NewReader(rFile)

	lh := md5.New()
	_, err := io.Copy(lh, lReader)
	lFileHash := lh.Sum(nil)
	Logf(INFO, "lFileLen:%v hash:%v err:%v", len(lFile), lFileHash, err)

	rh := md5.New()
	_, err = io.Copy(rh, rReader)
	rFileHash := rh.Sum(nil)
	Logf(INFO, "rFileLen:%v hash:%v err:%v", len(rFile), rFileHash, err)

	lReader.Seek(0, io.SeekStart)
	rReader.Seek(0, io.SeekStart)

	var serverStream *UDPStream
	var clientStream *UDPStream

	go func() {
		clientStream, err = clientTransport.Open(clientSel.PickAddrs(ipsCount))
		if err != nil {
			t.Fatalf("FileTransferClient open stream %v", err)
		}
		clientStream.SetNoDelay(1, 20, 2, 1)
		clientStream.SetWindowSize(32, 64)
	}()

	go func() {
		serverStream, err = serverTransport.Accept()
		if err != nil {
			t.Fatalf("FileTransferServer Accept stream %v", err)
		}
		serverStream.SetNoDelay(1, 20, 2, 1)
		serverStream.SetWindowSize(32, 64)
	}()

	for {
		if serverStream != nil && clientStream != nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	finish := make(chan struct{}, 2)
	go func() {
		FileTransfer(t, serverStream, rReader, lFileHash)
		finish <- struct{}{}
	}()

	go func() {
		FileTransfer(t, clientStream, lReader, rFileHash)
		finish <- struct{}{}
	}()

	<-finish
	<-finish
}

func TestAckXmit(t *testing.T) {
	kcp := NewKCP(1, func(buf []byte, size int, xmitMax uint32) {})
	for i := 0; i < IKCP_WND_RCV+2; i++ {
		kcp.ack_push(uint32(i), 0)
	}
	if len(kcp.ackxmitlist) != IKCP_WND_RCV {
		t.Fatal("ackxmitlist length failed")
	}

	var xmit uint32
	xmit = kcp.incre_ackxmit(1)
	if xmit != 0 {
		t.Fatal("xmit not expect")
	}
	xmit = kcp.incre_ackxmit(2)
	if xmit != 1 {
		t.Fatal("xmit not expect")
	}
	xmit = kcp.incre_ackxmit(IKCP_WND_RCV + 1)
	if xmit != 1 {
		t.Fatal("xmit not expect")
	}
	xmit = kcp.incre_ackxmit(IKCP_WND_RCV + 2)
	if xmit != 0 {
		t.Fatal("xmit not expect")
	}

	kcp.ack_push(IKCP_WND_RCV+1, 0)
	if len(kcp.ackxmitlist) != IKCP_WND_RCV {
		t.Fatal("TestAckXmit ackxmitlist length failed")
	}

	xmit = kcp.incre_ackxmit(IKCP_WND_RCV + 1)
	if xmit != 2 {
		t.Fatal("xmit not expect")
	}

	kcp.ack_push(IKCP_WND_RCV+3, 0)
	xmit = kcp.incre_ackxmit(2)
	if xmit != 0 {
		t.Fatal("xmit not expect")
	}
}

func BenchmarkFlush(b *testing.B) {
	kcp := NewKCP(1, func(buf []byte, size int, xmitMax uint32) {})
	kcp.snd_buf = make([]segment, 1024)
	for k := range kcp.snd_buf {
		kcp.snd_buf[k].xmit = 1
		kcp.snd_buf[k].resendts = currentMs() + 10000
	}
	b.ResetTimer()
	b.ReportAllocs()
	var mu sync.Mutex
	for i := 0; i < b.N; i++ {
		mu.Lock()
		kcp.flush(false)
		mu.Unlock()
	}
}

func BenchmarkMsgPushAndPop(b *testing.B) {
	tunnel, _ := NewUDPTunnel("127.0.0.1:0", nil)
	tunnel.Close()

	streamCount := 100

	msgss := make([][]ipv4.Message, streamCount)
	for i := 0; i < streamCount; i++ {
		msgCount := rand.Intn(5) + 1
		addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(rand.Intn(10)+1))
		msgs := make([]ipv4.Message, 0)
		for j := 0; j < msgCount; j++ {
			msgs = append(msgs, ipv4.Message{Addr: addr})
		}
		msgss[i] = msgs
	}

	b.ResetTimer()
	b.ReportAllocs()

	var msgssR [][]ipv4.Message
	for n := 0; n < b.N; n++ {
		wg := sync.WaitGroup{}
		wg.Add(streamCount)
		for i := 0; i < streamCount; i++ {
			go func(idx int) {
				defer wg.Done()
				tunnel.pushMsgs(msgss[idx])
			}(i)
		}
		wg.Wait()
		tunnel.popMsgss(&msgssR)
		msgssR = msgssR[:0]
	}
}

func BenchmarkEchoSpeed128B(b *testing.B) {
	InitLog(FATAL)
	echoSpeed(b, 128)
}

func BenchmarkEchoSpeed1K(b *testing.B) {
	InitLog(FATAL)
	echoSpeed(b, 1024)
}

func BenchmarkEchoSpeed4K(b *testing.B) {
	InitLog(FATAL)
	echoSpeed(b, 4096)
}

func BenchmarkEchoSpeed64K(b *testing.B) {
	InitLog(FATAL)
	echoSpeed(b, 65536)
}

func BenchmarkEchoSpeed512K(b *testing.B) {
	InitLog(FATAL)
	echoSpeed(b, 524288)
}

func BenchmarkEchoSpeed1M(b *testing.B) {
	InitLog(FATAL)
	echoSpeed(b, 1048576)
}

func BenchmarkSinkSpeed1K(b *testing.B) {
	InitLog(FATAL)
	sinkSpeed(b, 1024)
}
