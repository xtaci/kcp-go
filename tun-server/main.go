package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	kcp "github.com/ldcsoftware/kcp-go"
	"github.com/urfave/cli"
)

var logs [5]*log.Logger

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

	logs = [int(kcp.FATAL)]*log.Logger{Debug, Info, Warning, Error, Fatal}
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

func iobridge(dst io.Writer, src io.Reader) error {
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	for {
		n, err := src.Read(*buf)
		if err != nil {
			kcp.Logf(kcp.INFO, "iobridge reading err:%v n:%v", err, n)
			return err
		}

		_, err = dst.Write((*buf)[:n])
		if err != nil {
			kcp.Logf(kcp.INFO, "iobridge writing err:%v", err)
			return err
		}
	}
	return nil
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
		err := iobridge(s, conn)
		kcp.Logf(kcp.INFO, "toUDPStream stream:%v remote:%v err:%v", s.GetUUID(), conn.RemoteAddr(), err)
		if err == io.EOF {
			s.CloseWrite()
		}
		shutdown <- struct{}{}
	}

	toTCPStream := func(conn *net.TCPConn, s *kcp.UDPStream, shutdown chan struct{}) {
		err := iobridge(conn, s)
		kcp.Logf(kcp.INFO, "toTCPStream stream:%v remote:%v err:%v", s.GetUUID(), conn.RemoteAddr(), err)
		if err == io.EOF {
			conn.CloseWrite()
		}
		shutdown <- struct{}{}
	}

	go toUDPStream(s, conn, shutdown)
	toTCPStream(conn, s, shutdown)

	<-shutdown
	<-shutdown
}

func main() {
	myApp := cli.NewApp()
	myApp.Name = "tun-client"
	myApp.Version = "1.0.0"
	myApp.Flags = []cli.Flag{
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
	}
	myApp.Action = func(c *cli.Context) error {
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

		kcp.Logf = func(lvl kcp.LogLevel, f string, args ...interface{}) {
			if int(lvl) >= logLevel {
				logs[lvl-1].Printf(f+"\n", args...)
			}
		}

		sel, err := NewTestSelector()
		checkError(err)
		transport, err := kcp.NewUDPTransport(sel, kcp.FastKCPOption)
		checkError(err)
		for portS := localPortS; portS <= localPortE; portS++ {
			_, err := transport.NewTunnel(localIp+":"+strconv.Itoa(portS), nil)
			checkError(err)
		}

		for {
			stream, err := transport.Accept()
			checkError(err)
			stream.SetWindowSize(wndSize, wndSize)
			conn, err := net.Dial("tcp", targetAddr)
			checkError(err)
			tcpConn := conn.(*net.TCPConn)
			go handleClient(stream, tcpConn)
		}
		return nil
	}

	myApp.Run(os.Args)
}