package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	kcp "github.com/ldcsoftware/kcp-go"

	gouuid "github.com/satori/go.uuid"
)

var ips = flag.String("ips", "", "listen ips")
var portStart = flag.Int("portStart", 9111, "listen portStart")
var portEnd = flag.Int("portEnd", 9120, "listen portEnd")
var targetAddr = flag.String("targetAddr", "127.0.0.1:7900", "target address")

var netInterfaceIps = func() ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	ips := make([]string, 0)
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips, nil
}

func input_callback(tunnel *kcp.UDPTunnel, data []byte, addr net.Addr) {
	var uuid gouuid.UUID
	copy(uuid[:], data[:gouuid.Size])

	fmt.Printf("recv target uuid:%v remote:%v \n", uuid, addr)
}

type ss struct {
}

func (s *ss) test1() {
	fmt.Println("ss test1")
	s.test2()
}

func (s *ss) test2() {
	fmt.Println("ss test2")
}

type sss struct {
	*ss
}

func (s *sss) test1() {
	fmt.Println("sss test1")
	s.ss.test1()
}

func (s *sss) test2() {
	fmt.Println("sss test2")
}

func main() {
	buf := make([]byte, 500)
	for i := 0; i < len(buf); i++ {
		buf[i] = byte((i % 256))
	}
	fmt.Println(buf)
	return

	flag.Parse()

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

	logs := [int(kcp.FATAL) + 1]*log.Logger{Debug, Info, Warning, Error, Fatal}

	kcp.Logf = func(lvl kcp.LogLevel, f string, args ...interface{}) {
		logs[lvl].Printf(f+"\n", args...)
	}

	var ipList []string
	if *ips != "" {
		ipList = strings.Split(*ips, ",")
	} else {
		var err error
		ipList, err = netInterfaceIps()
		if err != nil {
			fmt.Println("netInterfaceIps error", err)
			return
		}
	}

	defaultRouterPortE := (*portStart + *portEnd) / 2

	fmt.Printf("portStart:%v\n", *portStart)
	fmt.Printf("portEnd:%v\n", *portEnd)
	fmt.Printf("defaultRouterPortE:%v\n", defaultRouterPortE)
	fmt.Printf("targetAddr:%v\n", *targetAddr)
	fmt.Printf("ipList:%v\n", ipList)

	defaultTunnels := make([]*kcp.UDPTunnel, 0)
	for port := *portStart; port <= defaultRouterPortE; port++ {
		tunnel, err := kcp.NewUDPTunnel(":"+strconv.Itoa(port), input_callback)
		if err != nil {
			fmt.Println("NewUDPTunnel error", err)
			return
		}
		defaultTunnels = append(defaultTunnels, tunnel)
	}

	for port := defaultRouterPortE + 1; port <= *portEnd; port++ {
		for _, ip := range ipList {
			_, err := kcp.NewUDPTunnel(ip+":"+strconv.Itoa(port), input_callback)
			if err != nil {
				fmt.Println("NewUDPTunnel error", err)
				return
			}
		}
	}

	// addr, err := net.ResolveUDPAddr("udp", *targetAddr)
	// if err != nil {
	// 	fmt.Println("ResolveUDPAddr error:", err)
	// 	return
	// }

	// idx := 0
	// for {
	// 	tunnel := defaultTunnels[idx]
	// 	idx = idx % len(defaultTunnels)

	// 	uuid, _ := gouuid.NewV1()
	// 	buf := make([]byte, 100)
	// 	copy(buf, uuid[:])

	// 	fmt.Printf("send target uuid:%v remote:%v \n", uuid, addr)

	// 	msg := ipv4.Message{}
	// 	msg.Buffers = [][]byte{buf}
	// 	msg.Addr = addr

	// 	time.Sleep(time.Second)
	// }
}
