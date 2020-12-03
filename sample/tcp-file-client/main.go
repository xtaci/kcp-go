package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func iobridge(dst io.Writer, src io.Reader) {
	buf := bufPool.Get().(*[]byte)
	for {
		n, err := src.Read(*buf)
		if n == 0 && err != nil {
			log.Printf("iobridge reading err:%v n:%v \n", err, n)
			break
		}

		_, err = dst.Write((*buf)[:n])
		if err != nil {
			log.Printf("iobridge writing err:%v \n", err)
			break
		}
	}
	bufPool.Put(buf)

	log.Printf("iobridge end \n")
}

type CloseWriteConn interface {
	CloseWrite() error
}

type nwriter struct {
	n int
}

func (nw *nwriter) Write(b []byte) (n int, err error) {
	nw.n += len(b)
	return n, err
}

type fileToStream struct {
	src  io.ReadSeeker
	conn CloseWriteConn
	md5  string
	once bool
	hmd5 hash.Hash
}

type fileInfo struct {
	Md5  string `json:"md5"`
	Size int    `json:"size"`
}

func (fs *fileToStream) Read(b []byte) (n int, err error) {
	if !fs.once {
		fs.once = true

		fs.hmd5 = md5.New()
		nw := &nwriter{}
		mw := io.MultiWriter(fs.hmd5, nw)
		_, err := io.Copy(mw, fs.src)
		tempHash := fs.hmd5.Sum(nil)
		fs.md5 = base64.StdEncoding.EncodeToString(tempHash)
		fs.src.Seek(0, io.SeekStart)

		fi := fileInfo{
			Md5:  fs.md5,
			Size: nw.n,
		}
		fib, err := json.Marshal(fi)
		if err != nil {
			log.Fatal("file info marshal failed. err:%v", err)
		}

		binary.LittleEndian.PutUint32(b, uint32(len(fib)))
		n = copy(b[4:], fib)
		if len(fib) != n {
			log.Fatalf("fileToStream write md5 failed. err:%v", err)
		}
		n += 4

		log.Printf("fileToStream size:%v md5:%v n:%v", nw.n, fs.md5, n)
		return n, nil
	}
	n, err = fs.src.Read(b)
	if err == io.EOF {
		fs.conn.CloseWrite()
	}
	return n, err
}

type streamToFile struct {
	size     int
	md5      string
	once     bool
	recvSize int
	recvMd5  string
	hmd5     hash.Hash
}

func (sf *streamToFile) Write(b []byte) (n int, err error) {
	if !sf.once {
		sf.once = true

		if len(b) < 4 {
			log.Fatalf("b less than 4")
		}
		filen := int(binary.LittleEndian.Uint32(b))
		if len(b) < 4+filen {
			log.Fatalf("b less then need:%v got:%v", 4+filen, len(b))
		}

		fi := fileInfo{}
		err := json.Unmarshal(b[4:4+filen], &fi)
		if err != nil {
			log.Fatal("file info unmarshal failed. err:%v", err)
		}
		sf.recvMd5 = fi.Md5
		sf.recvSize = fi.Size
		sf.hmd5 = md5.New()
		n = filen + 4

		log.Printf("streamToFile recvSize:%v recvMd5:%v n:%v left:%v \n", fi.Size, fi.Md5, n, len(b)-n)
		if n >= len(b) {
			return n, nil
		} else {
			b = b[n:]
		}
	}
	if len(sf.md5) != 0 {
		log.Fatalf("file recv wrong, md5 alreaddy calc. n:%v \n", len(b))
	}

	sf.size += len(b)
	nn, err := sf.hmd5.Write(b)
	n += nn
	if sf.size >= sf.recvSize {
		sf.md5 = base64.StdEncoding.EncodeToString(sf.hmd5.Sum(nil))
	}
	return n, err
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

func hanldeFileTransfer(conn *net.TCPConn, size int) {
	defer conn.Close()

	file := randString(size)
	reader := strings.NewReader(file)

	shutdown := make(chan bool, 2)

	//send file
	go func() {
		fs := &fileToStream{src: reader, conn: conn}
		iobridge(conn, fs)
		log.Printf("handleFileTransfer file send finish. md5:%v \n", fs.md5)

		shutdown <- true
	}()

	//recv file
	go func() {
		sf := &streamToFile{}
		iobridge(sf, conn)
		log.Printf("handleFileTransfer file recv finish. recvSize:%v recvMd5:%v size:%v md5:%v equal:%v \n",
			sf.recvSize, sf.recvMd5, sf.size, sf.md5, sf.recvMd5 == sf.md5)

		shutdown <- true
	}()

	<-shutdown
	<-shutdown

	log.Printf("hanldeFileTransfer finish \n")
	time.Sleep(time.Second * 5)
}

var addr = flag.String("addr", "127.0.0.1:7900", "input target address")
var size = flag.Int("size", 1024*1024, "file size")

func main() {
	flag.Parse()
	log.Printf("target addr:%v size:%v \n", *addr, *size)

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		fmt.Println("Error connecting:", err)
		os.Exit(1)
	}
	defer conn.Close()
	hanldeFileTransfer(conn.(*net.TCPConn), *size)
}
