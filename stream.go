// Package kcp-go is a Reliable-UDP library for golang.
//
// This library intents to provide a smooth, resilient, ordered,
// error-checked and anonymous delivery of streams over UDP packets.
//
// The interfaces of this package aims to be compatible with
// net.Conn in standard library, but offers powerful features for advanced users.
package kcp

import (
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
	"golang.org/x/net/ipv4"
)

var (
	errTimeout = errors.New("timeout")
)

const (
	SYN = '1'
	FIN = '2'
	PSH = '3'
)

type (
	// UDPStream defines a KCP session implemented by UDP
	UDPStream struct {
		uuid      gouuid.UUID
		sel       *RouteSelector
		transport *UDPTransport
		kcp       *KCP // KCP ARQ protocol
		ips       []string
		tunnels   []*UDPTunnel
		remotes   []net.Addr

		// kcp receiving is based on packets
		// recvbuf turns packets into stream
		recvbuf []byte
		sendbuf []byte
		bufptr  []byte

		// settings
		rd         time.Time // read deadline
		wd         time.Time // write deadline
		headerSize int       // the header size additional to a KCP frame
		ackNoDelay bool      // send ack immediately for each incoming packet(testing purpose)
		writeDelay bool      // delay kcp.flush() for Write() for bulk transfer

		// notifications
		die          chan struct{} // notify current session has Closed
		dieOnce      sync.Once
		chReadEvent  chan struct{} // notify Read() can be called without blocking
		chWriteEvent chan struct{} // notify Write() can be called without blocking

		// packets waiting to be sent on wire
		txqueues [][]ipv4.Message
		mu       sync.Mutex
	}
)

// newUDPSession create a new udp session for client or server
func NewUDPStream(uuid gouuid.UUID, ips []string, sel *RouteSelector, transport *UDPTransport, bool accepted) (stream *UDPStream, err error) {
	stream = new(UDPStream)
	stream.die = make(chan struct{})
	stream.chReadEvent = make(chan struct{}, 1)
	stream.chWriteEvent = make(chan struct{}, 1)
	stream.sendbuf = make([]byte, mtuLimit)
	stream.recvbuf = make([]byte, mtuLimit)
	stream.uuid = uuid
	stream.sel = sel
	stream.transport = transport
	stream.ips = ips
	stream.headerSize = gouuid.Size
	stream.txqueues = make([][]ipv4.Message, len(tunnels))

	stream.kcp = NewKCP(1, func(buf []byte, size int, lostSegs, fastRetransSegs, earlyRetransSegs uint64) {
		if size >= IKCP_OVERHEAD+stream.headerSize {
			stream.output(buf[:size], lostSegs, fastRetransSegs, earlyRetransSegs)
		}
	})
	stream.kcp.ReserveBytes(stream.headerSize)

	if !accepted {
		stream.tunnels, stream.remotes = sel.Pick(ips)
		stream.WriteCtrl(SYN, []byte{strings.Join(ips, ":")})
		SystemTimedSched.Put(sess.update, time.Now())
	}

	currestab := atomic.AddUint64(&DefaultSnmp.CurrEstab, 1)
	maxconn := atomic.LoadUint64(&DefaultSnmp.MaxConn)
	if currestab > maxconn {
		atomic.CompareAndSwapUint64(&DefaultSnmp.MaxConn, maxconn, currestab)
	}

	return stream, nil
}

// Read implements net.Conn
func (s *UDPStream) Read(b []byte) (n int, err error) {
	for {
		s.mu.Lock()
		if len(s.bufptr) > 0 { // copy from buffer into b
			n = copy(b, s.bufptr)
			s.bufptr = s.bufptr[n:]
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n))
			return n, nil
		}

		if size := s.kcp.PeekSize(); size > 0 { // peek data size from kcp
			// if necessary resize the stream buffer to guarantee a sufficent buffer space
			if cap(s.recvbuf) < size {
				s.recvbuf = make([]byte, size)
			}

			// resize the length of recvbuf to correspond to data size
			s.recvbuf = s.recvbuf[:size]
			s.kcp.Recv(s.recvbuf)
			n, err := s.packetRead(s.recvbuf[0], s.recvbuf[1:], b)
			s.bufptr = s.recvbuf[n+1:] // pointer update
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n+1))
			return n, nil
		}

		// deadline for current reading operation
		var timeout *time.Timer
		var c <-chan time.Time
		if !s.rd.IsZero() {
			if time.Now().After(s.rd) {
				s.mu.Unlock()
				return 0, errors.WithStack(errTimeout)
			}

			delay := s.rd.Sub(time.Now())
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		// wait for read event or timeout or error
		select {
		case <-s.chReadEvent:
			if timeout != nil {
				timeout.Stop()
			}
		case <-c:
			return 0, errors.WithStack(errTimeout)
		case <-s.die:
			return 0, errors.WithStack(io.ErrClosedPipe)
		}
	}
}

// Write implements net.Conn
func (s *UDPStream) Write(b []byte) (n int, err error) {
	return s.WriteBuffer(PSH, b)
}

// Write implements net.Conn
func (s *UDPStream) WriteCtrl(flag byte, b []byte) (n int, err error) {
	return s.WriteBuffer(flag, b)
}

// WriteBuffers write a vector of byte slices to the underlying connection
func (s *UDPStream) WriteBuffer(flag byte, b []byte) (n int, err error) {
	for {
		select {
		case <-s.die:
			return 0, errors.WithStack(io.ErrClosedPipe)
		default:
		}

		s.mu.Lock()

		// make sure write do not overflow the max sliding window on both side
		waitsnd := s.kcp.WaitSnd()
		if waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd) {
			n += len(b)
			for {
				if len(b) < int(s.kcp.mss) {
					s.sendbuf[0] = flag
					copy(s.sendbuf[1:], b)
					s.kcp.Send(s.sendbuf[0 : len(b)+1])
					break
				} else {
					s.sendbuf[0] = flag
					copy(s.sendbuf[1:], b[:s.kcp.mss-1])
					s.kcp.Send(s.sendbuf[:s.kcp.mss])
					b = b[s.kcp.mss-1:]
				}
			}

			waitsnd = s.kcp.WaitSnd()
			if waitsnd >= int(s.kcp.snd_wnd) || waitsnd >= int(s.kcp.rmt_wnd) || !s.writeDelay {
				s.kcp.flush(false)
			}
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesSent, uint64(n))
			return n, nil
		}

		var timeout *time.Timer
		var c <-chan time.Time
		if !s.wd.IsZero() {
			if time.Now().After(s.wd) {
				s.mu.Unlock()
				return 0, errors.WithStack(errTimeout)
			}
			delay := s.wd.Sub(time.Now())
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		select {
		case <-s.chWriteEvent:
			if timeout != nil {
				timeout.Stop()
			}
		case <-c:
			return 0, errors.WithStack(errTimeout)
		case <-s.die:
			return 0, errors.WithStack(io.ErrClosedPipe)
		}
	}
}

// uncork sends data in txqueue if there is any
func (s *UDPStream) uncork() {
	s.mu.Lock()
	txqueues = s.txqueues
	s.txqueues = make([]ipv4.Message, len(s.tunnels))
	s.mu.Unlock()

	//todo
	for i, txqueue := range txqueues {
		if len(txqueue) > 0 {
			s.tunnels[i].Output(txqueue)
		}
	}
}

// Close closes the connection.
func (s *UDPStream) Close() error {
	return s.close(true)
}

func (s *UDPStream) close(active bool) error {
	var once bool
	s.dieOnce.Do(func() {
		if active {
			s.WriteCtrl(FIN, nil)
		}
		close(s.die)
		once = true
	})

	if once {
		atomic.AddUint64(&DefaultSnmp.CurrEstab, ^uint64(0))

		// try best to send all queued messages
		s.mu.Lock()
		s.kcp.flush(false)
		// release pending segments
		s.kcp.ReleaseTX()
		s.mu.Unlock()
		s.uncork()

		return nil
	} else {
		return errors.WithStack(io.ErrClosedPipe)
	}
}

// sess update to trigger protocol
func (s *UDPStream) update() {
	select {
	case <-s.die:
	default:
		s.mu.Lock()
		interval := s.kcp.flush(false)
		waitsnd := s.kcp.WaitSnd()
		if waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd) {
			s.notifyWriteEvent()
		}
		s.mu.Unlock()
		s.uncork()
		// self-synchronized timed scheduling
		SystemTimedSched.Put(s.update, time.Now().Add(time.Duration(interval)*time.Millisecond))
	}
}

// SetDeadline sets the deadline associated with the listener. A zero time value disables the deadline.
func (s *UDPStream) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rd = t
	s.wd = t
	s.notifyReadEvent()
	s.notifyWriteEvent()
	return nil
}

// SetReadDeadline implements the Conn SetReadDeadline method.
func (s *UDPStream) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rd = t
	s.notifyReadEvent()
	return nil
}

// SetWriteDeadline implements the Conn SetWriteDeadline method.
func (s *UDPStream) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wd = t
	s.notifyWriteEvent()
	return nil
}

// SetWriteDelay delays write for bulk transfer until the next update interval
func (s *UDPStream) SetWriteDelay(delay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeDelay = delay
}

// SetWindowSize set maximum window size
func (s *UDPStream) SetWindowSize(sndwnd, rcvwnd int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.WndSize(sndwnd, rcvwnd)
}

// SetMtu sets the maximum transmission unit(not including UDP header)
func (s *UDPStream) SetMtu(mtu int) bool {
	if mtu > mtuLimit {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.SetMtu(mtu)
	return true
}

// SetStreamMode toggles the stream mode on/off
func (s *UDPStream) SetStreamMode(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enable {
		s.kcp.stream = 1
	} else {
		s.kcp.stream = 0
	}
}

// SetACKNoDelay changes ack flush option, set true to flush ack immediately,
func (s *UDPStream) SetACKNoDelay(nodelay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackNoDelay = nodelay
}

// SetNoDelay calls nodelay() of kcp
// https://github.com/skywind3000/kcp/blob/master/README.en.md#protocol-configuration
func (s *UDPStream) SetNoDelay(nodelay, interval, resend, nc int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.NoDelay(nodelay, interval, resend, nc)
}

// GetConv gets conversation id of a session
func (s *UDPStream) GetConv() uint32 { return s.kcp.conv }

func (s *UDPStream) notifyReadEvent() {
	select {
	case s.chReadEvent <- struct{}{}:
	default:
	}
}

func (s *UDPStream) notifyWriteEvent() {
	select {
	case s.chWriteEvent <- struct{}{}:
	default:
	}
}

func (s *UDPStream) fillMsg(buf []byte, remote net.Addr) (msg ipv4.Message) {
	bts := xmitBuf.Get().([]byte)[:len(buf)]
	copy(bts, buf)
	msg.Buffers = [][]byte{bts}
	msg.Addr = s.remote
	return
}

func (s *UDPStream) output(buf []byte, lostSegs, fastRetransSegs, earlyRetransSegs uint64) {
	copy(buf, s.uuid[:])
	appendCount = 1
	if lostSegs != 0 || fastRetransSegs != 0 || earlyRetransSegs != 0 {
		appendCount = len(s.tunnels)
	}
	for i := 0; i < appendCount; i++ {
		if len(s.txqueues[i]) == 0 {
			s.txqueues[i] = make([]ipv4.Message, 0)
		}
		s.txqueues[i] = append(s.txqueues[i], s.fillMsg(buf, s.remotes[i]))
	}
}

func (s *UDPStream) input(data []byte) {
	var kcpInErrors uint64

	s.mu.Lock()
	if ret := s.kcp.Input(data[gouuid.Size:], true, s.ackNoDelay); ret != 0 {
		kcpInErrors++
	}
	if n := s.kcp.PeekSize(); n > 0 {
		s.notifyReadEvent()
	}
	waitsnd := s.kcp.WaitSnd()
	if waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd) {
		s.notifyWriteEvent()
	}
	s.mu.Unlock()

	atomic.AddUint64(&DefaultSnmp.InPkts, 1)
	atomic.AddUint64(&DefaultSnmp.InBytes, uint64(len(data)))
	if kcpInErrors > 0 {
		atomic.AddUint64(&DefaultSnmp.KCPInErrors, kcpInErrors)
	}
}

func (s *UDPStream) packetRead(flag byte, data []byte, b []byte) (n int, err error) {
	switch flag {
	case SYN:
		return s.Syn(data)
	case FIN:
		return s.Fin(data)
	case PSH:
		return s.Psh(data, b)
	default:
		return 0, errors.New("invalid ctrl msg")
	}
}

func (s *UDPStream) Syn(data []byte) (n int, err error) {
	if len(s.ips) != 0 {
		return 0, errors.New("invalid syn packet, self ips not empty")
	}
	endpointInfo := string(data)
	ips := strings.Split(endpointInfo, ":")
	if len(ips) == 0 {
		return len(data), errors.New("invalid syn packet")
	}
	s.ips = ips
	s.tunnels, s.remotes = s.sel.Pick(ips)
	SystemTimedSched.Put(s.update, time.Now())
	return len(data), nil
}

func (s *UDPStream) Fin(data []byte) (n int, err error) {
	return 0, s.close(false)
}

func (s *UDPStream) Psh(data []byte, b []byte) (n int, err error) {
	return copy(b, data)
}
