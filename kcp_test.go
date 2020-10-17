package kcp

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
var clientStream *UDPStream
var clientTunnels []*UDPTunnel
var serverTransport *UDPTransport
var serverStream *UDPStream
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

	streamEstablish := make(chan struct{})
	go func() {
		serverStream, err = serverTransport.Accept()
		if err != nil {
			panic("serverTransport Accept")
		}
		streamEstablish <- struct{}{}
	}()

	clientStream, err = clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		panic("clientTransport Open")
	}
	<-streamEstablish
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

func server(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	serverStream.SetNoDelay(nodelay, interval, resend, nc)
	tunnelSimulate(serverTunnels, loss, delayMin, delayMax)

	buf := make([]byte, 65536)
	for {
		n, err := serverStream.Read(buf)
		if err != nil {
			return
		}
		serverStream.Write(buf[:n])
	}
}

func client(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	clientStream.SetNoDelay(nodelay, interval, resend, nc)
	tunnelSimulate(clientTunnels, loss, delayMin, delayMax)

	buf := make([]byte, 64)
	var rtt time.Duration
	for i := 0; i < repeat; i++ {
		start := time.Now()
		clientStream.Write(buf)
		io.ReadFull(clientStream, buf)
		rtt += time.Now().Sub(start)
	}

	Logf(INFO, "avg rtt:%v", rtt/repeat)
	Logf(INFO, "total time: %v for %v round trip:", rtt, repeat)
}

func testlink(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	Logf(INFO, "testing with nodelay parameters, loss:%v delayMin:%v delayMax:%v nodelay:%v interval:%v resend:%v nc:%v",
		loss, delayMin, delayMax, nodelay, interval, resend, nc)

	go server(t, loss, delayMin, delayMax, nodelay, interval, resend, nc)
	client(t, loss, delayMin, delayMax, nodelay, interval, resend, nc)
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

	tunnelSimulate(clientTunnels, 0, 0, 0)
	tunnelSimulate(serverTunnels, 0, 0, 0)
}

func TestTimeout(t *testing.T) {
	buf := make([]byte, 10)
	//timeout
	clientStream.SetDeadline(time.Now().Add(time.Second))
	<-time.After(2 * time.Second)
	n, err := clientStream.Read(buf)
	if n != 0 || err == nil {
		t.Fail()
	}
	clientStream.SetDeadline(time.Time{})
	Logf(INFO, "TestTimeout err:%v", err)
}

func TestSendRecv(t *testing.T) {
	go server(t, 0, 0, 0, 1, 10, 2, 0)
	clientStream.SetWriteDelay(true)

	const N = 100
	buf := make([]byte, 10)
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		clientStream.Write([]byte(msg))
		if n, err := clientStream.Read(buf); err == nil {
			if string(buf[:n]) != msg {
				t.Fatal("TestSendRecv msg not equal", err)
			}
		} else {
			t.Fatal("TestSendRecv", err)
		}
	}
}

func tinyRecvServer(t *testing.T) {
	buf := make([]byte, 2)
	for {
		n, err := serverStream.Read(buf)
		if err != nil {
			return
		}
		serverStream.Write(buf[:n])
	}
}

func TestTinyBufferReceiver(t *testing.T) {
	go tinyRecvServer(t)

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
		clientStream.Write(sndbuf)
		if n, err := io.ReadFull(clientStream, rcvbuf); err == nil {
			if !check(rcvbuf[:n]) {
				t.Fail()
			}
		} else {
			checkError(t, err)
		}
	}
}

func TestClose(t *testing.T) {
	var n int
	var err error

	clientStream.SetDeadline(time.Now().Add(time.Second))
	clientStream.CloseWrite()
	// write after close
	buf := make([]byte, 10)
	n, err = clientStream.Read(buf)
	if n != 0 || err != errTimeout {
		t.Fatal("Read after CloseWrite misbehavior")
	}

	n, err = clientStream.Write(buf)
	if n != 0 || err != io.ErrClosedPipe {
		t.Fatal("Write after CloseWrite misbehavior")
	}

	err = clientStream.Close()
	n, err = clientStream.Read(buf)
	if n != 0 || err != io.ErrClosedPipe {
		t.Fatal("Read after Close misbehavior")
	}

	err = clientStream.Close()
	if err != io.ErrClosedPipe {
		t.Fatal("Close after Close misbehavior")
	}
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
		if err != nil {
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
			if err != nil {
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

	parallel := false
	go func() {
		hp := clientTransport.pc.getHostParallel("127.0.0.1")
		for {
			time.Sleep(time.Second)
			parallel = hp.isParallel()
			if parallel {
				Logf(INFO, "enter global parallel")
				break
			}
		}
	}()
	wg.Wait()

	if !parallel {
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
		if err != nil {
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
	src    io.Reader
	stream *UDPStream
}

func (fs *fileToStream) Read(b []byte) (n int, err error) {
	n, err = fs.src.Read(b)
	if err == io.EOF {
		fs.stream.CloseWrite()
	}
	return n, err
}

func FileTransferClient(t *testing.T, lf io.Reader, rFileHash []byte) error {
	Logf(INFO, "FileTransferClient start")

	stream, err := clientTransport.Open(clientSel.PickAddrs(ipsCount))
	if err != nil {
		t.Fatalf("clientTransport open stream %v", err)
	}
	defer stream.Close()

	// bridge connection
	shutdown := make(chan bool, 2)

	go func() {
		fs := &fileToStream{lf, stream}
		iobridge(fs, stream)
		shutdown <- true

		Logf(INFO, "FileTransferClient file send finish")
	}()

	go func() {
		h := md5.New()
		iobridge(stream, h)
		recvHash := h.Sum(nil)
		if !bytes.Equal(rFileHash, recvHash) {
			t.Fatalf("client recv hash not equal fileHash:%v recvHash:%v", rFileHash, recvHash)
		}
		shutdown <- true

		Logf(INFO, "FileTransferClient file recv finish")
	}()

	<-shutdown
	<-shutdown
	return nil
}

func FileTransferServer(t *testing.T, rf io.Reader, lFileHash []byte) error {
	Logf(INFO, "FileTransferServer start")

	stream, err := serverTransport.Accept()
	if err != nil {
		t.Fatalf("server Accept stream %v", err)
	}
	defer stream.Close()

	// bridge connection
	shutdown := make(chan struct{}, 2)

	go func() {
		h := md5.New()
		iobridge(stream, h)
		recvHash := h.Sum(nil)
		if !bytes.Equal(lFileHash, recvHash) {
			t.Fatalf("server recv hash not equal fileHash:%v recvHash:%v", lFileHash, recvHash)
		}
		shutdown <- struct{}{}

		Logf(INFO, "FileTransferServer file recv finish")
	}()

	go func() {
		fs := &fileToStream{rf, stream}
		iobridge(fs, stream)
		shutdown <- struct{}{}

		Logf(INFO, "FileTransferServer file send finish")
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

func TestFileTransfer(t *testing.T) {
	lFile := randString(1024 * 512)
	lReader := strings.NewReader(lFile)
	rFile := randString(1024 * 1024)
	rReader := strings.NewReader(rFile)

	lh := md5.New()
	_, err := io.Copy(lh, lReader)
	lFileHash := lh.Sum(nil)
	Logf(INFO, "file:%v hash:%v err:%v", lFile, lFileHash, err)

	rh := md5.New()
	_, err = io.Copy(rh, rReader)
	rFileHash := rh.Sum(nil)
	Logf(INFO, "file:%v hash:%v err:%v", rFile, rFileHash, err)

	lReader.Seek(0, io.SeekStart)
	rReader.Seek(0, io.SeekStart)

	finish := make(chan struct{}, 2)
	go func() {
		FileTransferServer(t, rReader, lFileHash)
		finish <- struct{}{}
	}()

	time.Sleep(time.Millisecond * 300)

	go func() {
		FileTransferClient(t, lReader, rFileHash)
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
