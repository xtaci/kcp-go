package main

import (
	"fmt"
	"log"
	"net"
	"os"
)

func checkError(err error) {
	if err != nil {
		fmt.Println("checkError", err)
		os.Exit(-1)
	}
}

func handleEcho(conn *net.TCPConn) {
	fmt.Println("handleEcho start", conn.RemoteAddr())
	defer fmt.Println("handleEcho end", conn.RemoteAddr())

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

func main() {
	addr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:7900")
	checkError(err)
	listener, err := net.ListenTCP("tcp", addr)
	checkError(err)

	fmt.Println("server start", addr)
	for {
		conn, err := listener.AcceptTCP()
		checkError(err)
		go handleEcho(conn)
	}
}
