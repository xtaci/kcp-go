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

type (
	// UDPStream defines a KCP session implemented by UDP
	UDPStream struct {
		uuid         gouuid.UUID
		tunnel       *UDPTunnel
		backupTunnel *UDPTunnel
		kcp          *KCP // KCP ARQ protocol
		remote       net.Addr
		backupRemote net.Addr

		// kcp receiving is based on packets
		// recvbuf turns packets into stream
		recvbuf []byte
		bufptr  []byte

		// settings
		rd         time.Time // read deadline
		wd         time.Time // write deadline
		headerSize int       // the header size additional to a KCP frame
		ackNoDelay bool      // send ack immediately for each incoming packet(testing purpose)
		writeDelay bool      // delay kcp.flush() for Write() for bulk transfer
		dup        int       // duplicate udp packets(testing purpose)

		// notifications
		die          chan struct{} // notify current session has Closed
		dieOnce      sync.Once
		chReadEvent  chan struct{} // notify Read() can be called without blocking
		chWriteEvent chan struct{} // notify Write() can be called without blocking

		// packets waiting to be sent on wire
		txqueue       []ipv4.Message
		backupTxqueue []ipv4.Message
		mu            sync.Mutex
	}
)

// newUDPSession create a new udp session for client or server
func NewUDPStream(uuid gouuid.UUID, tunnel, backupTunnel *UDPTunnel, remote, backupRemote net.Addr) (stream *UDPStream, err error) {
	stream = new(UDPStream)
	stream.die = make(chan struct{})
	stream.chReadEvent = make(chan struct{}, 1)
	stream.chWriteEvent = make(chan struct{}, 1)
	stream.recvbuf = make([]byte, mtuLimit)
	stream.uuid = uuid
	stream.tunnel = tunnel
	stream.backupTunnel = backupTunnel
	stream.remote = remote
	stream.backupRemote = backupRemote
	stream.headerSize = gouuid.Size

	stream.kcp = NewKCP(1, func(buf []byte, size int, lostSegs, fastRetransSegs, earlyRetransSegs uint64) {
		if size >= IKCP_OVERHEAD+stream.headerSize {
			stream.output(buf[:size], lostSegs, fastRetransSegs, earlyRetransSegs)
		}
	})
	stream.kcp.ReserveBytes(stream.headerSize)

	currestab := atomic.AddUint64(&DefaultSnmp.CurrEstab, 1)
	maxconn := atomic.LoadUint64(&DefaultSnmp.MaxConn)
	if currestab > maxconn {
		atomic.CompareAndSwapUint64(&DefaultSnmp.MaxConn, maxconn, currestab)
	}

	return stream, nil
}

func (s *UDPStream) Dial() (err error) {

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
			if len(b) >= size { // receive data into 'b' directly
				s.kcp.Recv(b)
				s.mu.Unlock()
				atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(size))
				return size, nil
			}

			// if necessary resize the stream buffer to guarantee a sufficent buffer space
			if cap(s.recvbuf) < size {
				s.recvbuf = make([]byte, size)
			}

			// resize the length of recvbuf to correspond to data size
			s.recvbuf = s.recvbuf[:size]
			s.kcp.Recv(s.recvbuf)
			n = copy(b, s.recvbuf)   // copy to 'b'
			s.bufptr = s.recvbuf[n:] // pointer update
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n))
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
func (s *UDPStream) Write(b []byte) (n int, err error) { return s.WriteBuffers([][]byte{b}) }

// WriteBuffers write a vector of byte slices to the underlying connection
func (s *UDPStream) WriteBuffers(v [][]byte) (n int, err error) {
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
			for _, b := range v {
				n += len(b)
				for {
					if len(b) <= int(s.kcp.mss) {
						s.kcp.Send(b)
						break
					} else {
						s.kcp.Send(b[:s.kcp.mss])
						b = b[s.kcp.mss:]
					}
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
	txqueue := s.txqueue
	s.txqueue = s.txqueue[:0]
	backupTxqueue := s.backupTxqueue
	s.backupTxqueue = s.backupTxqueue[:0]
	s.mu.Unlock()

	//todo
	if len(txqueue) > 0 {
		s.tunnel.Output(txqueue)
	}
	if len(backupTxqueue) > 0 {
		s.backupTunnel.Output(backupTxqueue)
	}
}

// Close closes the connection.
func (s *UDPStream) Close() error {
	var once bool
	s.dieOnce.Do(func() {
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

// (deprecated)
//
// SetDUP duplicates udp packets for kcp output.
func (s *UDPStream) SetDUP(dup int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dup = dup
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

// post-processing for sending a packet from kcp core
// steps:
// 1. FEC packet generation
// 2. CRC32 integrity
// 3. Encryption
// 4. TxQueue
func (s *UDPStream) fillMsg(buf []byte, remote net.Addr) (msg ipv4.Message) {
	bts := xmitBuf.Get().([]byte)[:len(buf)]
	copy(bts, buf)
	msg.Buffers = [][]byte{bts}
	msg.Addr = s.remote
	return
}

func (s *UDPStream) output(buf []byte, lostSegs, fastRetransSegs, earlyRetransSegs uint64) {
	copy(buf, s.uuid[:])
	s.txqueue = append(s.txqueue, s.fillMsg(buf, s.remote))
	if lostSegs != 0 || fastRetransSegs != 0 || earlyRetransSegs != 0 {
		s.backupTxqueue = append(s.backupTxqueue, s.fillMsg(buf, s.backupRemote))
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
