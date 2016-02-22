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
	remote  *net.UDPAddr
	udpconn *net.UDPConn
	kcp     *KCP
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
type Listener struct {
	conn     *net.UDPConn
	sessions map[string]*UDPSession
	accepts  chan *UDPSession
	stop     chan struct{}
	buffer   []byte
}

// main loop
func (l *Listener) loop() {
	for {
	}

}

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.accepts:
		return net.Conn(c), nil
	case <-l.stop:
		return nil, errors.New("listener stopped")
	}
}

func (l *Listener) Close() error {
	close(l.stop)
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
