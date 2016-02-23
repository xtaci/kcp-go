package kcp

import (
	"errors"
	"net"
	"time"
)

const (
	BUFSIZE = 4096
)

// Implement net.Conn for KCP
type UDPSession struct {
	die chan struct{}
	kcp *KCP
}

func NewUDPSession(conv uint32, conn *net.UDPConn, addr *net.UDPAddr) *UDPSession {
	sess := new(UDPSession)
	sess.kcp = NewKCP(conv, func(buf []byte, size int) {
		conn.WriteToUDP(buf[:size], addr)
	})
	return sess
}

func (s *UDPSession) Read(b []byte) (n int, err error) {
	return 0, nil
}

func (s *UDPSession) Write(b []byte) (n int, err error) {
	return 0, nil
}

func (s *UDPSession) Close() error {
	return nil
}

func (s *UDPSession) LocalAddr() net.Addr {
	return nil
}

func (s *UDPSession) RemoteAddr() net.Addr {
	return nil
}

func (s *UDPSession) SetDeadline(t time.Time) error {
	return nil
}

func (s *UDPSession) SetReadDeadline(t time.Time) error {
	return nil
}

func (s *UDPSession) SetWriteDeadline(t time.Time) error {
	return nil
}

// Session Manager Implement net.Listener
type (
	Listener struct {
		conn     *net.UDPConn
		sessions map[string]*UDPSession
		accepts  chan *UDPSession
		die      chan struct{}
		buffer   []byte
	}

	Packet struct {
		data []byte
		addr *net.UDPAddr
	}
)

// main loop
func (l *Listener) loop() {
	ticker := time.NewTicker(10 * time.Millisecond)
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
					l.sessions[addr] = NewUDPSession(conv, l.conn, pkt.addr)
				}
			}
			sess.Read(pkt.data)
		case <-ticker.C:
			for k := range l.sessions {
				l.sessions[k].kcp.Update(uint32(time.Now().Nanosecond() / int(time.Millisecond)))
			}
		}
	}
}

func (l *Listener) read_loop() chan Packet {
	ch := make(chan Packet, 128)
	go func(ch chan Packet) {
		for {
			if n, from, err := l.conn.ReadFromUDP(l.buffer); err == nil {
				data := make([]byte, n)
				copy(data, l.buffer)
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

	listener := &Listener{}
	listener.conn = conn
	listener.sessions = make(map[string]*UDPSession)
	listener.buffer = make([]byte, BUFSIZE)
	go listener.loop()
	return listener, nil
}
