package kcp

import (
	"errors"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

var (
	ERR_TIMEOUT          = errors.New("i/o timeout")
	ERR_BROKEN_PIPE      = errors.New("broken pipe")
	ERR_PACKET_TOO_LARGE = errors.New("packet too large")
)

const (
	MODE_DEFAULT = 0
	MODE_NORMAL  = 1
	MODE_FAST    = 2
)

type (
	UDPSession struct {
		kcp           *KCP         // the core ARQ
		conn          *net.UDPConn // the underlying UDP socket
		l             *Listener    // point to server listener if it's a server socket
		local, remote net.Addr
		rd            time.Time // read deadline
		sockbuff      []byte    // kcp receiving is based on packet, I turn it into stream
		die           chan struct{}
		mu            sync.Mutex
	}
)

//  create a new udp session for client or server
func newUDPSession(conv uint32, mode int, l *Listener, conn *net.UDPConn, remote *net.UDPAddr) *UDPSession {
	sess := new(UDPSession)
	sess.die = make(chan struct{})
	sess.local = conn.LocalAddr()
	sess.remote = remote
	sess.conn = conn
	sess.l = l
	sess.kcp = NewKCP(conv, func(buf []byte, size int) {
		n, err := conn.WriteToUDP(buf[:size], remote)
		if err != nil {
			log.Println(err, n)
		}
	})
	sess.kcp.WndSize(128, 128)
	switch mode {
	case MODE_FAST:
		sess.kcp.NoDelay(1, 10, 2, 1)
	case MODE_NORMAL:
		sess.kcp.NoDelay(0, 10, 0, 1)
	default:
		sess.kcp.NoDelay(0, 10, 0, 1)
	}

	go sess.update_task()
	if l == nil { // it's a client connection
		go sess.read_loop()
	}
	return sess
}

// Read implements the Conn Read method.
func (s *UDPSession) Read(b []byte) (n int, err error) {
	ticker := time.NewTimer(20 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if len(s.sockbuff) > 0 { // copy from buffer
			n := copy(b, s.sockbuff)
			s.sockbuff = s.sockbuff[n:]
			return n, nil
		}

		select {
		case <-s.die: // closed connection
			return -1, ERR_BROKEN_PIPE
		default:
		}

		s.mu.Lock()
		if !s.rd.IsZero() {
			if time.Now().After(s.rd) { // timeout
				s.mu.Unlock()
				return -1, ERR_TIMEOUT
			}
		}

		if n := s.kcp.PeekSize(); n > 0 { // data arrived
			buf := make([]byte, n)
			if s.kcp.Recv(buf) > 0 { // if Recv() succeded
				n := copy(b, buf)
				s.sockbuff = buf[n:] // store remaining bytes into sockbuff for next read
				s.mu.Unlock()
				return n, nil
			}
		}
		s.mu.Unlock()
		ticker.Reset(20 * time.Millisecond)
	}
	return -1, ERR_BROKEN_PIPE
}

// Write implements the Conn Write method.
func (s *UDPSession) Write(b []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.die:
		return -1, ERR_BROKEN_PIPE
	default:
		n = s.kcp.Send(b)
		if n == -2 {
			return n, ERR_PACKET_TOO_LARGE
		}
	}
	return
}

// Close closes the connection.
func (s *UDPSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.die:
		return ERR_BROKEN_PIPE
	default:
		close(s.die)
		if s.l == nil { // client socket close
			s.conn.Close()
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rd = t
	return nil
}

// SetReadDeadline implements the Conn SetReadDeadline method.
func (s *UDPSession) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
			s.mu.Lock()
			s.kcp.Update(uint32(time.Now().UnixNano() / int64(time.Millisecond)))
			/*
				if s.kcp.state != 0 { // deadlink
					close(s.die)
				}
			*/
			s.mu.Unlock()
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
			s.mu.Lock()
			s.kcp.Input(data)
			s.mu.Unlock()
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
		mode         int
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
			s, ok := l.sessions[addr]
			if !ok {
				var conv uint32
				if len(data) >= IKCP_OVERHEAD {
					ikcp_decode32u(data, &conv) // conversation id
					log.Println("conv id:", conv)
					s := newUDPSession(conv, l.mode, l, conn, from)
					s.mu.Lock()
					s.kcp.Input(data)
					s.mu.Unlock()
					l.sessions[addr] = s
					l.ch_accepts <- s
				}
			} else {
				s.mu.Lock()
				s.kcp.Input(data)
				s.mu.Unlock()
			}
		}

		select {
		case deadlink := <-l.ch_deadlinks: // remove deadlinks
			delete(l.sessions, deadlink.String())
		case <-l.die: // listener close
			return
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
	if err := l.conn.Close(); err == nil {
		close(l.die)
		return nil
	} else {
		return err
	}
}

// Addr returns the listener's network address, The Addr returned is shared by all invocations of Addr, so do not modify it.
func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// Listen listens for incoming KCP packets addressed to the local address laddr on the network "udp"
// mode must be one of:
// MODE_DEFAULT
// MODE_NORMAL
// MODE_FAST
func Listen(mode int, laddr string) (*Listener, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", laddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		return nil, err
	}

	l := new(Listener)
	l.conn = conn
	l.mode = mode
	l.sessions = make(map[string]*UDPSession)
	l.ch_accepts = make(chan *UDPSession, 10)
	l.ch_deadlinks = make(chan net.Addr, 10)
	l.die = make(chan struct{})
	go l.monitor()
	return l, nil
}

// Dial connects to the remote address raddr on the network "udp"
// mode must be one of:
// MODE_DEFAULT
// MODE_NORMAL
// MODE_FAST
func Dial(mode int, raddr string) (*UDPSession, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, err
	}
	udpconn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	return newUDPSession(rand.Uint32(), mode, nil, udpconn, udpaddr), nil
}
