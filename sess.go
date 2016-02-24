package kcp

import (
	"errors"
	"net"
	"sync"
	"time"
)

const (
	BUFSIZE = 4096
)

var (
	TIMEOUT = errors.New("Deadline exceeded")
)

// Implement net.Conn for KCP
type (
	w struct {
		data []byte
		ok   chan struct{}
	}

	UDPSession struct {
		kcp           *KCP
		local, remote net.Addr
		read_deadline time.Time
		closed        bool
		die           chan struct{}
		sync.Mutex
	}
)

func NewUDPSession(conv uint32, conn *net.UDPConn, addr *net.UDPAddr) *UDPSession {
	sess := new(UDPSession)
	sess.local = conn.LocalAddr()
	sess.remote = addr
	sess.kcp = NewKCP(conv, func(buf []byte, size int) {
		conn.WriteToUDP(buf[:size], addr)
	})
	sess.kcp.WndSize(128, 128)
	sess.kcp.NoDelay(1, 10, 2, 1)
	go sess.monitor()
	return sess
}

func (s *UDPSession) Read(b []byte) (n int, err error) {
	for {
		if !s.read_deadline.IsZero() {
			if time.Now().Before(s.read_deadline) {
				return 0, TIMEOUT
			}
		}

		s.Lock()
		if s.kcp.PeekSize() > 0 {
			return s.kcp.Recv(b), nil
			s.Unlock()
		} else {
			s.Unlock()
			<-time.After(10 * time.Millisecond)
		}
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

func (s *UDPSession) Input(data []byte) {
	s.Lock()
	defer s.Unlock()
	s.kcp.Input(data)
}

func (s *UDPSession) monitor() {
	ticker := time.NewTicker(10 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			s.Lock()
			s.kcp.Update(uint32(time.Now().Nanosecond() / int(time.Millisecond)))
			s.Unlock()
		case <-s.die:
			return
		}
	}
}

// Session Manager Implement net.Listener
type (
	Listener struct {
		conn     *net.UDPConn
		sessions map[string]*UDPSession
		accepts  chan *UDPSession
		die      chan struct{}
	}

	Packet struct {
		data []byte
		addr *net.UDPAddr
	}
)

func (l *Listener) monitor() {
	ch := l.read_loop()
	for {
		select {
		case pkt := <-ch:
			addr := pkt.addr.String()
			sess, ok := l.sessions[addr]
			if !ok {
				var conv uint32
				if len(pkt.data) >= IKCP_OVERHEAD {
					ikcp_decode32u(pkt.data, &conv) // conversation id
					sess := NewUDPSession(conv, l.conn, pkt.addr)
					sess.Input(pkt.data)
					l.sessions[addr] = sess
					l.accepts <- sess
				}
			} else {
				sess.Input(pkt.data)
			}
		}
	}
}

func (l *Listener) read_loop() chan Packet {
	ch := make(chan Packet, 128)
	buffer := make([]byte, 4096)
	go func(ch chan Packet) {
		for {
			if n, from, err := l.conn.ReadFromUDP(buffer); err == nil {
				data := make([]byte, n)
				copy(data, buffer[:n])
				ch <- Packet{data, from}
			}

		}
	}(ch)
	return ch
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

func Listen(addr string) (*Listener, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, _err := net.ListenUDP("udp", udpaddr)
	if _err != nil {
		return nil, _err
	}

	l := &Listener{}
	l.conn = conn
	l.sessions = make(map[string]*UDPSession)
	l.accepts = make(chan *UDPSession)
	go l.monitor()
	return l, nil
}
