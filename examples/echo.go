// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"crypto/sha1"
	"io"
	"log"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
)

func main() {
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)

	listener, err := kcp.ListenWithOptions("127.0.0.1:12345", block, 10, 3)
	if err != nil {
		log.Fatal(err)
		return
	}
	defer listener.Close()

	// Start the client.
	go client()
	for {
		s, err := listener.AcceptKCP()
		if err != nil {
			log.Fatal(err)
			return
		}

		go handleEcho(s)
	}
}

// handleEcho sends back everything it receives.
func handleEcho(conn *kcp.UDPSession) {
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}

		_, err = conn.Write(buf[:n])
		if err != nil {
			log.Println(err)
			return
		}
	}
}

func client() {
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)

	// wait for server to become ready
	time.Sleep(time.Second)

	// dial to the echo server
	sess, err := kcp.DialWithOptions("127.0.0.1:12345", block, 10, 3)
	if err != nil {
		log.Fatal(err)
		return
	}
	defer sess.Close()

	for {
		data := time.Now().String()
		buf := make([]byte, len(data))
		log.Println("sent:", data)

		_, err := sess.Write([]byte(data))
		if err != nil {
			log.Fatal(err)
			return
		}

		// read back the data
		_, err = io.ReadFull(sess, buf)
		if err != nil {
			log.Fatal(err)
			return
		}

		log.Println("recv:", string(buf))
		time.Sleep(time.Second)
	}
}
