package kcp

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

var (
	ERR_TIMEOUT = errors.New("Deadline exceeded")
)

type (
	UDPSession struct {
		kcp           *KCP
		conn          *net.UDPConn
		ch_in         chan []byte
		local, remote net.Addr
		read_deadline time.Time
		closed        bool
		die           chan struct{}
		sync.Mutex
	}
)

func NewUDPSession(conv uint32, isclient bool, conn *net.UDPConn, remote *net.UDPAddr) *UDPSession {
	sess := new(UDPSession)
	sess.local = conn.LocalAddr()
	sess.remote = remote
	sess.conn = conn
	sess.ch_in = make(chan []byte, 10)
	sess.kcp = NewKCP(conv, func(buf []byte, size int) {
		n, err := conn.WriteToUDP(buf[:size], remote)
		if err != nil {
			log.Println(err, n)
		}
	})
	sess.kcp.WndSize(128, 128)
	sess.kcp.NoDelay(0, 10, 0, 1)
	go sess.update_task()
	if isclient {
		go sess.read_loop()
	}
	return sess
}

func (s *UDPSession) Read(b []byte) (n int, err error) {
	for {
		s.Lock()
		if !s.read_deadline.IsZero() {
			if time.Now().Before(s.read_deadline) {
				s.Unlock()
				return -1, ERR_TIMEOUT
			}
		}

		if s.kcp.PeekSize() > 0 {
			n = s.kcp.Recv(b)
			s.Unlock()
			return n, nil
		}

		s.Unlock()
		<-time.After(20 * time.Millisecond)
	}
}

func (s *UDPSession) Write(b []byte) (n int, err error) {
	s.Lock()
	defer s.Unlock()
	return s.kcp.Send(b), nil
}

func (s *UDPSession) Close() error {
	s.Lock()
	defer s.Unlock()
	if !s.closed {
		close(s.die)
		s.closed = true
	}
	return nil
}

func (s *UDPSession) LocalAddr() net.Addr {
	return s.local
}

func (s *UDPSession) RemoteAddr() net.Addr {
	return s.remote
}

func (s *UDPSession) SetDeadline(t time.Time) error {
	s.Lock()
	defer s.Unlock()
	s.read_deadline = t
	return nil
}

func (s *UDPSession) SetReadDeadline(t time.Time) error {
	s.Lock()
	defer s.Unlock()
	s.read_deadline = t
	return nil
}

func (s *UDPSession) SetWriteDeadline(t time.Time) error {
	return nil
}

func (s *UDPSession) update_task() {
	ticker := time.NewTicker(10 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			s.Lock()
			s.kcp.Update(uint32(time.Now().UnixNano() / int64(time.Millisecond)))
			s.Unlock()
		case data := <-s.ch_in:
			s.Lock()
			s.kcp.Input(data)
			s.Unlock()
		case <-s.die:
			return
		}
	}
}

func (s *UDPSession) read_loop() {
	conn := s.conn
	buffer := make([]byte, 4096)
	for {
		if n, err := conn.Read(buffer); err == nil {
			data := make([]byte, n)
			copy(data, buffer[:n])
			s.ch_in <- data
		}
	}
}

type (
	Listener struct {
		conn     *net.UDPConn
		sessions map[string]*UDPSession
		accepts  chan *UDPSession
		die      chan struct{}
	}
)

func (l *Listener) monitor() {
	conn := l.conn
	buffer := make([]byte, 4096)
	for {
		if n, from, err := conn.ReadFromUDP(buffer); err == nil {
			data := make([]byte, n)
			copy(data, buffer[:n])
			addr := from.String()
			sess, ok := l.sessions[addr]
			if !ok {
				var conv uint32
				if len(data) >= IKCP_OVERHEAD {
					ikcp_decode32u(data, &conv) // conversation id
					fmt.Println("conv id:", conv)
					sess := NewUDPSession(conv, false, conn, from)
					sess.ch_in <- data
					l.sessions[addr] = sess
					l.accepts <- sess
				}
			} else {
				sess.ch_in <- data
			}
		}
	}
}

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.accepts:
		return net.Conn(c), nil
	case <-l.die:
		return nil, errors.New("listener stopped")
	}
}

func (l *Listener) Close() error {
	close(l.die)
	return nil
}

func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// kcp listen
func Listen(addr string) (*Listener, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		return nil, err
	}

	l := &Listener{}
	l.conn = conn
	l.sessions = make(map[string]*UDPSession)
	l.accepts = make(chan *UDPSession, 10)
	go l.monitor()
	return l, nil
}

// dial
func Dial(addr string) (*UDPSession, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpconn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	return NewUDPSession(rand.Uint32(), true, udpconn, udpaddr), nil
}
