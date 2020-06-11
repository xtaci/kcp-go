package kcp

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type TunnelPoll struct {
	tunnels []*UDPTunnel
	idx     uint32
}

func (poll *TunnelPoll) AddTunnel(tunnel *UDPTunnel) {
	poll.tunnels = append(poll.tunnels, tunnel)
}

func (poll *TunnelPoll) PickTunnel() (tunnel *UDPTunnel) {
	idx := atomic.AddUint32(&poll.idx, 1) % uint32(len(poll.tunnels))
	return poll.tunnels[idx]
}

type TestSelector struct {
	tunnelIPM   map[string]*TunnelPoll
	remoteAddrs []net.Addr
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

func (sel *TestSelector) AddTunnel(tunnel *UDPTunnel) {
	localIp := tunnel.LocalIp()
	poll, ok := sel.tunnelIPM[localIp]
	if !ok {
		poll = &TunnelPoll{
			tunnels: make([]*UDPTunnel, 0),
		}
		sel.tunnelIPM[localIp] = poll
	}
	poll.AddTunnel(tunnel)
}

func (sel *TestSelector) Pick(remoteIps []string) (tunnels []*UDPTunnel, remotes []net.Addr) {
	tunnels = make([]*UDPTunnel, 0)
	for _, remoteIp := range remoteIps {
		tunnelPoll, ok := sel.tunnelIPM[remoteIp]
		if ok {
			tunnels = append(tunnels, tunnelPoll.PickTunnel())
		}
	}
	return tunnels, sel.remoteAddrs[:int(len(tunnels))]
}

var clientStream *UDPStream
var clientTunnels []*UDPTunnel
var serverStream *UDPStream
var serverTunnels []*UDPTunnel

func init() {
	Logf = func(lvl LogLevel, f string, args ...interface{}) {
		if lvl < INFO {
			return
		}
		fmt.Printf(f+"\n", args...)
	}
	Logf(INFO, "init")
	DefaultDialTimeout = time.Second * 2

	// var lAddrs = []string{"127.0.0.1:17001", "127.0.0.1:17002"}
	// var rAddrs = []string{"127.0.0.1:18001", "127.0.0.1:18002"}
	// var remoteIps = []string{"127.0.0.1", "127.0.0.1"}
	var lAddrs = []string{"127.0.0.1:17001"}
	var rAddrs = []string{"127.0.0.1:18001"}
	var remoteIps = []string{"127.0.0.1"}

	clientSel, err := NewTestSelector(rAddrs)
	if err != nil {
		panic("NewTestSelector")
	}
	clientTransport, err := NewUDPTransport(clientSel, nil, false)
	if err != nil {
		panic("NewUDPTransport")
	}
	for _, lAddr := range lAddrs {
		tunnel, err := clientTransport.NewTunnel(lAddr)
		if err != nil {
			panic("NewTunnel")
		}
		clientSel.AddTunnel(tunnel)
		clientTunnels = append(clientTunnels, tunnel)
	}

	serverSel, err := NewTestSelector(lAddrs)
	if err != nil {
		panic("NewTestSelector")
	}
	serverTransport, err := NewUDPTransport(serverSel, nil, true)
	if err != nil {
		panic("NewUDPTransport")
	}
	for _, rAddr := range rAddrs {
		tunnel, err := serverTransport.NewTunnel(rAddr)
		if err != nil {
			panic("NewTunnel")
		}
		serverSel.AddTunnel(tunnel)
		serverTunnels = append(serverTunnels, tunnel)
	}

	clientStream, err = clientTransport.Open(remoteIps)
	if err != nil {
		panic("clientTransport Open")
	}

	serverStream, err = serverTransport.Accept()
	if err != nil {
		panic("serverTransport Accept")
	}
}

const repeat = 16

func TestLossyConn1(t *testing.T) {
	Logf(INFO, "testing loss rate 0.1, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 1")
	testlink(t, 0.1, 10, 20, 1, 10, 2, 1)
}

func TestLossyConn2(t *testing.T) {
	Logf(INFO, "testing loss rate 0.2, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 1")
	testlink(t, 0.2, 10, 20, 1, 10, 2, 1)
}

func TestLossyConn3(t *testing.T) {
	Logf(INFO, "testing loss rate 0.3, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 1")
	testlink(t, 0.3, 10, 20, 1, 10, 2, 1)
}

func TestLossyConn4(t *testing.T) {
	Logf(INFO, "testing loss rate 0.1, rtt 20-40ms")
	Logf(INFO, "testing link with nodelay parameters:1 10 2 0")
	testlink(t, 0.1, 10, 20, 1, 10, 2, 0)
}

func checkError(t *testing.T, err error) {
	if err != nil {
		Logf(ERROR, "checkError: %+v\n", err)
		t.Fatal(err)
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

func testlink(t *testing.T, loss float64, delayMin, delayMax int, nodelay, interval, resend, nc int) {
	Logf(INFO, "testing with nodelay parameters, loss:%v delayMin:%v delayMax:%v nodelay:%v interval:%v resend:%v nc:%v",
		loss, delayMin, delayMax, nodelay, interval, resend, nc)

	go server(t, loss, delayMin, delayMax, nodelay, interval, resend, nc)
	client(t, loss, delayMin, delayMax, nodelay, interval, resend, nc)
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
