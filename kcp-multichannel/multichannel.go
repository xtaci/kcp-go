package main

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

func init() {
	Debug := log.New(os.Stdout,
		"DEBUG: ",
		log.Ldate|log.Ltime)

	Info := log.New(os.Stdout,
		"INFO: ",
		log.Ldate|log.Ltime)

	Warning := log.New(os.Stdout,
		"WARNING: ",
		log.Ldate|log.Ltime)

	Error := log.New(os.Stdout,
		"ERROR: ",
		log.Ldate|log.Ltime)

	Fatal := log.New(os.Stdout,
		"FATAL: ",
		log.Ldate|log.Ltime)

	logs := [int(kcp.FATAL)]*log.Logger{Debug, Info, Warning, Error, Fatal}

	kcp.Logf = func(lvl kcp.LogLevel, f string, args ...interface{}) {
		logs[lvl-1].Printf(f+"\n", args...)
	}
}

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

var lAddrs = []string{"127.0.0.1:7001", "127.0.0.1:7002"}
var lFile = "./file/lFile"
var rFileSave = "./file/rFileSave"

var rAddrs = []string{"127.0.0.1:8001", "127.0.0.1:8002"}
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
	sel, err := NewTestSelector(rAddrs)
	if err != nil {
		kcp.Logf(kcp.ERROR, "Client NewTestSelector err:%v", err)
		return
	}
	transport, err := kcp.NewUDPTransport(sel, nil, false)
	if err != nil {
		kcp.Logf(kcp.ERROR, "Client NewUDPTransport err:%v", err)
		return
	}
	var closeTunnel *kcp.UDPTunnel
	for _, lAddr := range lAddrs {
		tunnel, err := transport.NewTunnel(lAddr)
		if err != nil {
			panic("NewTunnel")
		}
		tunnel.Simulate(0, 0, 0)
		if closeTunnel == nil {
			closeTunnel = tunnel
		}
	}
	stream, err := transport.Open([]string{"127.0.0.1", "127.0.0.1"})
	if err != nil {
		kcp.Logf(kcp.ERROR, "Client transport open err:%v", err)
		return
	}

	go func() {
		time.Sleep(time.Second * 5)
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
	fmt.Println("ServeClientStream End", stream.GetUUID())
}

func Server() {
	sel, err := NewTestSelector(lAddrs)
	if err != nil {
		kcp.Logf(kcp.ERROR, "Server NewTestSelector err:%v", err)
		return
	}

	transport, err := kcp.NewUDPTransport(sel, nil, true)
	if err != nil {
		kcp.Logf(kcp.ERROR, "Server transport open err:%v", err)
		return
	}

	for _, rAddr := range rAddrs {
		tunnel, err := transport.NewTunnel(rAddr)
		if err != nil {
			panic("NewTunnel")
		}
		tunnel.Simulate(0, 0, 0)
	}

	for {
		stream, _ := transport.Accept()
		go ServeServerStream(stream)
	}
}

func ServeServerStream(stream *kcp.UDPStream) {
	kcp.Logf(kcp.INFO, "ServeServerStream start uuid:%v", stream.GetUUID())

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

	kcp.Logf(kcp.INFO, "ServeServerStream end uuid:%v hashCheck:%v", stream.GetUUID(), bytes.Equal(lFileHash, lFileSaveHash))
}

func main() {
	quit := make(chan int)

	go Server()
	time.Sleep(time.Second)
	go Client()

	quit <- 1
}
