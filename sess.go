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
	ERR_TIMEOUT     = errors.New("i/o timeout")
	ERR_BROKEN_PIPE = errors.New("broken pipe")
)

type (
	UDPSession struct {
		kcp           *KCP         // the core ARQ
		conn          *net.UDPConn // the underlying UDP socket
		l             *Listener    // point to server listener if it's a server socket
		ch_in         chan []byte  // input data from UDP socket
		local, remote net.Addr
		rd            time.Time // read deadline
		die           chan struct{}
		bts           []byte
		sync.Mutex
	}
)

//  create a new udp session for client or server
func newUDPSession(conv uint32, l *Listener, conn *net.UDPConn, remote *net.UDPAddr) *UDPSession {
	sess := new(UDPSession)
	sess.local = conn.LocalAddr()
	sess.remote = remote
	sess.conn = conn
	sess.l = l
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
	if l == nil {
		go sess.read_loop()
	}
	return sess
}

// Read implements the Conn Read method.
func (s *UDPSession) Read(b []byte) (n int, err error) {
	for {
		if len(s.bts) > 0 {
			n := copy(b, s.bts)
			s.bts = s.bts[n:]
			return n, nil
		}

		s.Lock()
		select {
		case <-s.die: // closed connection
			s.Unlock()
			return -1, ERR_BROKEN_PIPE
		default:
		}

		if !s.rd.IsZero() {
			if time.Now().After(s.rd) { // timeout
				s.Unlock()
				return -1, ERR_TIMEOUT
			}
		}

		if n := s.kcp.PeekSize(); n > 0 { // data arrived
			buf := make([]byte, n)
			if s.kcp.Recv(buf) > 0 { // buffer large enough
				n := copy(b, buf)
				s.bts = buf[n:]
				s.Unlock()
				return n, nil
			}
		}
		s.Unlock()
		<-time.After(20 * time.Millisecond)
	}
}

// Write implements the Conn Write method.
func (s *UDPSession) Write(b []byte) (n int, err error) {
	s.Lock()
	defer s.Unlock()
	return s.kcp.Send(b), nil
}

// Close closes the connection.
func (s *UDPSession) Close() error {
	s.Lock()
	defer s.Unlock()
	select {
	case <-s.die:
	default:
		close(s.die)
	}
	return nil
}

// LocalAddr returns the local network address. The Addr returned is shared by all invocations of LocalAddr, so do not modify it.
func (s *UDPSession) LocalAddr() net.Addr {
	return s.local
}

// RemoteAddr returns the remote network address. The Addr returned is shared by all invocations of RemoteAddr, so do not modify it.
func (s *UDPSession) RemoteAddr() net.Addr { return s.remote }

// SetDeadline sets the deadline associated with the listener. A zero time value disables the deadline.
func (s *UDPSession) SetDeadline(t time.Time) error {
	s.Lock()
	defer s.Unlock()
	s.rd = t
	return nil
}

// SetReadDeadline implements the Conn SetReadDeadline method.
func (s *UDPSession) SetReadDeadline(t time.Time) error {
	s.Lock()
	defer s.Unlock()
	s.rd = t
	return nil
}

// SetWriteDeadline implements the Conn SetWriteDeadline method.
func (s *UDPSession) SetWriteDeadline(t time.Time) error {
	return nil
}

// kcp update, input loop
func (s *UDPSession) update_task() {
	ticker := time.NewTicker(10 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			s.Lock()
			s.kcp.Update(uint32(time.Now().UnixNano() / int64(time.Millisecond)))
			state := s.kcp.state
			s.Unlock()
			if state != 0 { // deadlink
				close(s.die)
			}
		case data := <-s.ch_in:
			s.Lock()
			s.kcp.Input(data)
			s.Unlock()
		case <-s.die:
			if s.l != nil { // has listener
				s.l.ch_deadlinks <- s.remote
			}
			return
		}
	}
}

// read loop for client session
func (s *UDPSession) read_loop() {
	conn := s.conn
	buffer := make([]byte, 4096)
	for {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		if n, err := conn.Read(buffer); err == nil {
			data := make([]byte, n)
			copy(data, buffer[:n])
			s.ch_in <- data
		} else if err, ok := err.(*net.OpError); ok && err.Timeout() {
		} else {
			return
		}

		select {
		case <-s.die:
			return
		default:
		}
	}
}

type (
	Listener struct {
		conn         *net.UDPConn
		sessions     map[string]*UDPSession
		ch_accepts   chan *UDPSession
		ch_deadlinks chan net.Addr
		die          chan struct{}
	}
)

// monitor incoming data for all connections of server
func (l *Listener) monitor() {
	conn := l.conn
	buffer := make([]byte, 4096)
	for {
		conn.SetReadDeadline(time.Now().Add(time.Second))
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
					sess := newUDPSession(conv, l, conn, from)
					sess.ch_in <- data
					l.sessions[addr] = sess
					l.ch_accepts <- sess
				}
			} else {
				sess.ch_in <- data
			}
		}

		select {
		case deadlink := <-l.ch_deadlinks:
			delete(l.sessions, deadlink.String())
		default:
		}
	}
}

// Accept implements the Accept method in the Listener interface; it waits for the next call and returns a generic Conn.
func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch_accepts:
		return net.Conn(c), nil
	case <-l.die:
		return nil, errors.New("listener stopped")
	}
}

// Close stops listening on the TCP address. Already Accepted connections are not closed.
func (l *Listener) Close() error {
	l.conn.Close()
	close(l.die)
	return nil
}

// Addr returns the listener's network address, The Addr returned is shared by all invocations of Addr, so do not modify it.
func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// Listen listens for incoming KCP packets addressed to the local address laddr
func Listen(laddr string) (*Listener, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", laddr)
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
	l.ch_accepts = make(chan *UDPSession, 10)
	l.ch_deadlinks = make(chan net.Addr, 10)
	go l.monitor()
	return l, nil
}

// Dial connects to the remote address raddr on the network net
func Dial(raddr string) (*UDPSession, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, err
	}
	udpconn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	return newUDPSession(rand.Uint32(), nil, udpconn, udpaddr), nil
}
