package main

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

type TunnelPoll struct {
	tunnels []*kcp.UDPTunnel
	idx     uint32
}

func (poll *TunnelPoll) AddTunnel(tunnel *kcp.UDPTunnel) {
	poll.tunnels = append(poll.tunnels, tunnel)
}

func (poll *TunnelPoll) PickTunnel() (tunnel *kcp.UDPTunnel) {
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

func (sel *TestSelector) AddTunnel(tunnel *kcp.UDPTunnel) {
	localIp := tunnel.LocalIp()
	poll, ok := sel.tunnelIPM[localIp]
	if !ok {
		poll = &TunnelPoll{
			tunnels: make([]*kcp.UDPTunnel, 0),
		}
		sel.tunnelIPM[localIp] = poll
	}
	poll.AddTunnel(tunnel)
}

func (sel *TestSelector) Pick(remoteIps []string) (tunnels []*kcp.UDPTunnel, remotes []net.Addr) {
	tunnels = make([]*kcp.UDPTunnel, 0)
	for _, remoteIp := range remoteIps {
		tunnelPoll, ok := sel.tunnelIPM[remoteIp]
		if ok {
			tunnels = append(tunnels, tunnelPoll.PickTunnel())
		}
	}
	return tunnels, sel.remoteAddrs[:int(len(tunnels))]
}

var lAddrs = []string{"192.168.1.5:7001", "192.168.1.5:7002"}
var lFile = "./file/lFile"
var rFileSave = "./file/rFileSave"

var rAddrs = []string{"192.168.1.5:8001", "192.168.1.5:8002"}
var rFile = "./file/rFile"
var lFileSave = "./file/lFileSave"

var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func iobridge(src io.Reader, dst io.Writer, shutdown chan bool) {
	defer func() {
		shutdown <- true
	}()

	buf := bufPool.Get().(*[]byte)
	for {
		n, err := src.Read(*buf)
		if err != nil {
			fmt.Printf("error reading err:%v n:%v", err, n)
			fmt.Println("")
			break
		}

		_, err = dst.Write((*buf)[:n])
		if err != nil {
			fmt.Printf("error writing err:%v", err)
			fmt.Println("")
			break
		}
	}
	bufPool.Put(buf)

	fmt.Println("iobridge end")
}

func Client() {
	sel, _ := NewTestSelector(rAddrs)
	transport, _ := kcp.NewUDPTransport(sel, nil)
	var closeTunnel *kcp.UDPTunnel
	for _, lAddr := range lAddrs {
		tunnel, _ := transport.NewTunnel(lAddr)
		tunnel.Simulate(0, 0, 0)
		if closeTunnel == nil {
			closeTunnel = tunnel
		}
	}
	stream, _ := transport.Open([]string{"192.168.1.5", "192.168.1.5"})

	go func() {
		time.Sleep(time.Second * 5)
		fmt.Println("Close Tunnel Test")
		closeTunnel.Close()
	}()

	ServeClientStream(stream)
}

var lFileHash []byte
var lFileSaveHash []byte

func ServeClientStream(stream *kcp.UDPStream) {
	fmt.Println("ServeClientStream Start", stream.GetUUID())

	// bridge connection
	shutdown := make(chan bool, 2)
	h := md5.New()

	lf, _ := os.Open(lFile)
	defer lf.Close()
	tr := io.TeeReader(lf, h)
	go iobridge(tr, stream, shutdown)

	// rf, _ := os.Create(rFileSave)
	// defer rf.Close()
	// go iobridge(stream, rf, shutdown)

	// <-shutdown
	<-shutdown
	lFileHash = h.Sum(nil)
	stream.Close()

	for {
		if stream.IsClean() {
			break
		} else {
			time.Sleep(time.Millisecond * 10)
		}
	}

	fmt.Println("ServeClientStream End", stream.GetUUID())
}

func Server() {
	sel, _ := NewTestSelector(lAddrs)
	transport, _ := kcp.NewUDPTransport(sel, nil)
	for _, rAddr := range rAddrs {
		tunnel, _ := transport.NewTunnel(rAddr)
		tunnel.Simulate(0, 0, 0)
	}

	for {
		stream, _ := transport.Accept()
		go ServeServerStream(stream)
	}
}

func ServeServerStream(stream *kcp.UDPStream) {
	fmt.Println("ServeServerStream Start", stream.GetUUID())

	// bridge connection
	shutdown := make(chan bool, 2)
	h := md5.New()

	// lf, _ := os.Open(rFile)
	// defer lf.Close()
	// go iobridge(lf, stream, shutdown)

	rf, _ := os.Create(lFileSave)
	tr := io.TeeReader(stream, h)
	defer rf.Close()
	go iobridge(tr, rf, shutdown)

	// <-shutdown
	<-shutdown
	lFileSaveHash = h.Sum(nil)
	stream.Close()

	for {
		if stream.IsClean() {
			break
		} else {
			time.Sleep(time.Millisecond * 10)
		}
	}

	fmt.Println("ServeServerStream End", stream.GetUUID(), bytes.Equal(lFileHash, lFileSaveHash))
}

func main() {
	kcp.KCPLogf = func(lvl kcp.LogLevel, f string, args ...interface{}) {
		fmt.Printf(f, args...)
		fmt.Println()
	}

	fmt.Println("Start")
	quit := make(chan int)

	go Client()
	go Server()

	quit <- 1
}
