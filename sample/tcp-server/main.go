package main

import (
	"flag"
	"log"
	"net"
	_ "net/http/pprof"
	"os"
)

func checkError(err error) {
	if err != nil {
		log.Println("checkError", err)
		os.Exit(-1)
	}
}

func handleEcho(conn *net.TCPConn) {
	log.Println("handleEcho start", conn.RemoteAddr())
	defer log.Println("handleEcho end", conn.RemoteAddr())

	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}

		n, err = conn.Write(buf[:n])
		if err != nil {
			log.Println(err)
			return
		}
	}
}

var listenAddr = flag.String("listenAddr", "0.0.0.0:7900", "listen address")

func main() {
	flag.Parse()
	log.Printf("listenAddr:%v\n", *listenAddr)

	addr, err := net.ResolveTCPAddr("tcp", *listenAddr)
	checkError(err)
	listener, err := net.ListenTCP("tcp", addr)
	checkError(err)

	log.Println("server start", addr)
	for {
		conn, err := listener.AcceptTCP()
		checkError(err)
		go handleEcho(conn)
	}
}
