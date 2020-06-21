package kcp

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type TunnelPoll struct {
	tunnels []*UDPTunnel
	idx     uint32
}

func (poll *TunnelPoll) Add(tunnel *UDPTunnel) {
	poll.tunnels = append(poll.tunnels, tunnel)
}

func (poll *TunnelPoll) Pick() (tunnel *UDPTunnel) {
	idx := atomic.AddUint32(&poll.idx, 1) % uint32(len(poll.tunnels))
	return poll.tunnels[idx]
}

type TestSelector struct {
	tunnelIPM   map[string]*TunnelPoll
	remoteAddrs []net.Addr
	remoteIdx   uint32
}

func NewTestSelector(remotes []string) (*TestSelector, error) {
	remoteAddrs := make([]net.Addr, len(remotes))
	for i := 0; i < len(remotes); i++ {
		addr, err := net.ResolveUDPAddr("udp", remotes[i])
		if err != nil {
			return nil, err
		}
		remoteAddrs[i] = addr
	}
	return &TestSelector{
		tunnelIPM:   make(map[string]*TunnelPoll),
		remoteAddrs: remoteAddrs,
	}, nil
}

func (sel *TestSelector) Add(tunnel *UDPTunnel) {
	localIp := tunnel.LocalAddr().IP.String()
	poll, ok := sel.tunnelIPM[localIp]
	if !ok {
		poll = &TunnelPoll{
			tunnels: make([]*UDPTunnel, 0),
		}
		sel.tunnelIPM[localIp] = poll
	}
	poll.Add(tunnel)
}

func (sel *TestSelector) PickRemotes(count int) (remotes []string) {
	for i := 0; i < count; i++ {
		idx := atomic.AddUint32(&sel.remoteIdx, 1) % uint32(len(sel.remoteAddrs))
		remotes = append(remotes, sel.remoteAddrs[idx].String())
	}
	return remotes
}

func (sel *TestSelector) Pick(remotes []string) (tunnels []*UDPTunnel) {
	tunnels = make([]*UDPTunnel, 0)
	for _, remote := range remotes {
		remoteAddr, err := net.ResolveUDPAddr("udp", remote)
		if err == nil {
			tunnelPoll, ok := sel.tunnelIPM[remoteAddr.IP.String()]
			if ok {
				tunnels = append(tunnels, tunnelPoll.Pick())
			}
		}
	}
	return tunnels
}

var lPortStart = 7001
var lPortCount = 10
var rPortStart = 17001
var rPortCount = 10

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

func init() {
	Logf = func(lvl LogLevel, f string, args ...interface{}) {
		if lvl < INFO {
			return
		}
		// fmt.Printf(f+"\n", args...)
	}

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
	DefaultDialTimeout = time.Second * 2

	var err error
	clientSel, err = NewTestSelector(rAddrs)
	if err != nil {
		panic("NewTestSelector")
	}
	clientTransport, err = NewUDPTransport(clientSel, nil)
	if err != nil {
		panic("NewUDPTransport")
	}
	for _, lAddr := range lAddrs {
		tunnel, err := clientTransport.NewTunnel(lAddr, nil)
		if err != nil {
			panic("NewTunnel")
		}
		clientTunnels = append(clientTunnels, tunnel)
	}

	serverSel, err := NewTestSelector(lAddrs)
	if err != nil {
		panic("NewTestSelector")
	}
	serverTransport, err = NewUDPTransport(serverSel, nil)
	if err != nil {
		panic("NewUDPTransport")
	}
	for _, rAddr := range rAddrs {
		tunnel, err := serverTransport.NewTunnel(rAddr, nil)
		if err != nil {
			panic("NewTunnel")
		}
		serverTunnels = append(serverTunnels, tunnel)
	}

	clientStream, err = clientTransport.Open(clientSel.PickRemotes(len(remoteIps)))
	if err != nil {
		panic("clientTransport Open")
	}

	serverStream, err = serverTransport.Accept()
	if err != nil {
		panic("serverTransport Accept")
	}
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

func server(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	serverStream.SetNoDelay(nodelay, interval, resend, nc)
	serverTunnels[0].Simulate(loss, delayMin, delayMax)

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
	clientTunnels[0].Simulate(loss, delayMin, delayMax)

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

func testTimeout(t *testing.T) {
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

func testSendRecv(t *testing.T) {
	go server(t, 0.1, 10, 20, 1, 10, 2, 0)
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

func tinyRecvServer(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	serverStream.SetNoDelay(nodelay, interval, resend, nc)
	serverTunnels[0].Simulate(loss, delayMin, delayMax)

	buf := make([]byte, 2)
	for {
		n, err := serverStream.Read(buf)
		if err != nil {
			return
		}
		serverStream.Write(buf[:n])
	}
}

func testTinyBufferReceiver(t *testing.T) {
	go tinyRecvServer(t, 0.1, 10, 20, 1, 10, 2, 0)

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

func testClose(t *testing.T) {
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

func parallelServer(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	serverTunnels[0].Simulate(loss, delayMin, delayMax)

	for {
		stream, err := serverTransport.Accept()
		stream.SetNoDelay(nodelay, interval, resend, nc)
		checkError(t, err)
		go handleServerStream(stream)
	}
}

func handleServerStream(stream *UDPStream) {
	defer stream.Close()

	buf := make([]byte, 2)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			return
		}
		stream.Write(buf[:n])
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
	for {
		stream, _ := serverTransport.Accept()
		go handleEchoClient(stream)
	}
}

func sinkServer() {
	for {
		stream, _ := serverTransport.Accept()
		go handleSinkClient(stream)
	}
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
	stream, err := clientTransport.Open(clientSel.PickRemotes(len(remoteIps)))
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
	stream, err := clientTransport.Open(clientSel.PickRemotes(len(remoteIps)))
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

func parallelClient(t *testing.T, wg *sync.WaitGroup) (err error) {
	defer wg.Done()
	err = echoClient(64, 64)
	if err != nil {
		t.Fatal("parallelClient", err)
	}
	return
}

func testParallel1024CLIENT_64BMSG_64CNT(t *testing.T) {
	go echoServer()

	var wg sync.WaitGroup
	N := 1000
	wg.Add(N)
	for i := 0; i < N; i++ {
		go parallelClient(t, &wg)
	}
	wg.Wait()
}

func testSNMP(t *testing.T) {
	Logf(INFO, "DefaultSnmp.Copy:%v", DefaultSnmp.Copy())
	Logf(INFO, "DefaultSnmp.Header:%v", DefaultSnmp.Header())
	Logf(INFO, "DefaultSnmp.ToSlice:%v", DefaultSnmp.ToSlice())
	DefaultSnmp.Reset()
	Logf(INFO, "DefaultSnmp.ToSlice:%v", DefaultSnmp.ToSlice())
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

func TestKCP(t *testing.T) {
	Logf(INFO, "TestKCP.testLossyConn1")
	testLossyConn1(t)

	Logf(INFO, "TestKCP.testLossyConn2")
	testLossyConn2(t)

	Logf(INFO, "TestKCP.testLossyConn3")
	testLossyConn3(t)

	Logf(INFO, "TestKCP.testLossyConn4")
	testLossyConn4(t)

	Logf(INFO, "TestKCP.testTimeout")
	testTimeout(t)

	Logf(INFO, "TestKCP.testSendRecv")
	testSendRecv(t)

	Logf(INFO, "TestKCP.testTinyBufferReceiver")
	testTinyBufferReceiver(t)

	Logf(INFO, "TestKCP.testClose")
	testClose(t)

	Logf(INFO, "TestKCP.testParallel1024CLIENT_64BMSG_64CNT")
	testParallel1024CLIENT_64BMSG_64CNT(t)

	Logf(INFO, "TestKCP.testSNMP")
	testSNMP(t)
}

func BenchmarkEchoSpeed128B(b *testing.B) {
	echoSpeed(b, 128)
}

func BenchmarkEchoSpeed1K(b *testing.B) {
	echoSpeed(b, 1024)
}

func BenchmarkEchoSpeed4K(b *testing.B) {
	echoSpeed(b, 4096)
}

func BenchmarkEchoSpeed64K(b *testing.B) {
	echoSpeed(b, 65536)
}

func BenchmarkEchoSpeed512K(b *testing.B) {
	echoSpeed(b, 524288)
}

func BenchmarkEchoSpeed1M(b *testing.B) {
	echoSpeed(b, 1048576)
}

func BenchmarkSinkSpeed1K(b *testing.B) {
	sinkSpeed(b, 1024)
}
