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
		ch_in         chan []byte
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
	sess.ch_in = make(chan []byte, 10)
	sess.kcp = NewKCP(conv, func(buf []byte, size int) {
		conn.WriteToUDP(buf[:size], addr)
	})
	sess.kcp.WndSize(128, 128)
	sess.kcp.NoDelay(1, 10, 2, 1)
	go sess.update_task()
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

func (s *UDPSession) update_task() {
	ticker := time.NewTicker(10 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			s.Lock()
			s.kcp.Update(uint32(time.Now().Nanosecond() / int(time.Millisecond)))
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
	buffer := make([]byte, 4096)
	for {
		if n, from, err := l.conn.ReadFromUDP(buffer); err == nil {
			data := make([]byte, n)
			copy(data, buffer[:n])
			addr := from.String()
			sess, ok := l.sessions[addr]
			if !ok {
				var conv uint32
				if len(data) >= IKCP_OVERHEAD {
					ikcp_decode32u(data, &conv) // conversation id
					sess := NewUDPSession(conv, l.conn, from)
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
