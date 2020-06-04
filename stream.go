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
	errTimeout    = errors.New("timeout")
	errRemoteIps  = errors.New("err remote ips")
	errStreamFlag = errors.New("err stream flag")
	errSynInfo    = errors.New("err syn info")
)

const (
	PSH = '1'
	SYN = '2'
	FIN = '3'
	HRT = '4'
	RST = '5'
)

const (
	DialCheckInterval = 20
	FlagOffset        = gouuid.Size + IKCP_OVERHEAD
	CleanTimeout      = time.Second * 5
	HrtTimeout        = time.Second * 30
)

type clean_callback func(uuid gouuid.UUID)

type (
	// UDPStream defines a KCP session implemented by UDP
	UDPStream struct {
		uuid      gouuid.UUID
		sel       RouteSelector
		kcp       *KCP     // KCP ARQ protocol
		remoteIps []string //remoteips
		tunnels   []*UDPTunnel
		remotes   []net.Addr
		accepted  bool
		cleancb   clean_callback

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
		recvSynOnce    sync.Once
		sendFinOnce    sync.Once
		chSendFinEvent chan struct{} // notify send fin
		recvFinOnce    sync.Once
		chRecvFinEvent chan struct{} // notify recv fin
		rstOnce        sync.Once
		chRst          chan struct{} // notify current session has Closed
		closeOnce      sync.Once
		chClose        chan struct{} // notify current session has Closed
		chClean        chan struct{} // notify current session has cleand

		chReadEvent  chan struct{} // notify Read() can be called without blocking
		chWriteEvent chan struct{} // notify Write() can be called without blocking

		hrtTicker *time.Ticker

		// packets waiting to be sent on wire
		msgss [][]ipv4.Message
		mu    sync.Mutex
	}
)

// newUDPSession create a new udp session for client or server
func NewUDPStream(uuid gouuid.UUID, accepted bool, remoteIps []string, sel RouteSelector, cleancb clean_callback) (stream *UDPStream, err error) {
	tunnels, remotes := sel.Pick(remoteIps)
	if len(tunnels) == 0 || len(tunnels) != len(remotes) {
		return nil, errRemoteIps
	}

	stream = new(UDPStream)
	stream.chClose = make(chan struct{})
	stream.chRst = make(chan struct{})
	stream.chSendFinEvent = make(chan struct{})
	stream.chRecvFinEvent = make(chan struct{})
	stream.chClean = make(chan struct{})
	stream.chReadEvent = make(chan struct{}, 1)
	stream.chWriteEvent = make(chan struct{}, 1)
	stream.sendbuf = make([]byte, mtuLimit)
	stream.recvbuf = make([]byte, mtuLimit)
	stream.uuid = uuid
	stream.sel = sel
	stream.cleancb = cleancb
	stream.headerSize = gouuid.Size
	stream.msgss = make([][]ipv4.Message, 0)
	stream.accepted = accepted
	stream.hrtTicker = time.NewTicker(HrtTimeout)
	stream.remoteIps = remoteIps
	stream.tunnels = tunnels
	stream.remotes = remotes

	stream.kcp = NewKCP(1, func(buf []byte, size int, lostSegs, fastRetransSegs, earlyRetransSegs uint64) {
		if size >= IKCP_OVERHEAD+stream.headerSize {
			stream.output(buf[:size], lostSegs, fastRetransSegs, earlyRetransSegs)
		}
	})
	stream.kcp.ReserveBytes(stream.headerSize)

	if !accepted {
		localIps := make([]string, len(stream.tunnels))
		for i := 0; i < len(stream.tunnels); i++ {
			localIps[i] = stream.tunnels[i].LocalIp()
		}
		stream.WriteFlag(SYN, []byte(strings.Join(localIps, ":")))
		stream.kcp.flush(false)
		stream.uncork()
	}

	currestab := atomic.AddUint64(&DefaultSnmp.CurrEstab, 1)
	maxconn := atomic.LoadUint64(&DefaultSnmp.MaxConn)
	if currestab > maxconn {
		atomic.CompareAndSwapUint64(&DefaultSnmp.MaxConn, maxconn, currestab)
	}

	SystemTimedSched.Put(stream.update, time.Now())
	Logf(INFO, "NewUDPStream uuid:%v accepted:%v remoteIps:%v", uuid, accepted, remoteIps)
	return stream, nil
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

func (s *UDPStream) SetDeadLink(deadLink int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.dead_link = uint32(deadLink)
}

// GetConv gets conversation id of a session
func (s *UDPStream) GetConv() uint32      { return s.kcp.conv }
func (s *UDPStream) GetUUID() gouuid.UUID { return s.uuid }

// Read implements net.Conn
func (s *UDPStream) Read(b []byte) (n int, err error) {
	for {
		select {
		case <-s.chClose:
			return 0, io.ErrClosedPipe
		case <-s.chRst:
			return 0, io.ErrUnexpectedEOF
		case <-s.chRecvFinEvent:
			return 0, io.EOF
		default:
		}

		s.mu.Lock()
		if len(s.bufptr) > 0 { // copy from buffer into b, ctrl msg should not cache into this
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
			flag := s.recvbuf[0]
			n, err := s.cmdRead(flag, s.recvbuf[1:], b)
			s.bufptr = s.recvbuf[n+1:] // pointer update
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n+1))
			if flag != PSH {
				n = 0
			}
			return n, err
		}

		// deadline for current reading operation
		var timeout *time.Timer
		var c <-chan time.Time
		if !s.rd.IsZero() {
			if time.Now().After(s.rd) {
				s.mu.Unlock()
				return 0, errTimeout
			}

			delay := s.rd.Sub(time.Now())
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		// wait for read event or timeout or error
		select {
		case <-s.chClose:
			return 0, io.ErrClosedPipe
		case <-s.chRst:
			return 0, io.ErrUnexpectedEOF
		case <-s.chRecvFinEvent:
			return 0, io.EOF
		case <-s.chReadEvent:
			if timeout != nil {
				timeout.Stop()
			}
		case <-c:
			return 0, errTimeout
		}
	}
}

// Write implements net.Conn
func (s *UDPStream) Write(b []byte) (n int, err error) {
	return s.WriteBuffer(PSH, b)
}

// Write implements net.Conn
func (s *UDPStream) WriteFlag(flag byte, b []byte) (n int, err error) {
	return s.WriteBuffer(flag, b)
}

// WriteBuffers write a vector of byte slices to the underlying connection
func (s *UDPStream) WriteBuffer(flag byte, b []byte) (n int, err error) {
	for {
		select {
		case <-s.chClose:
			return 0, io.ErrClosedPipe
		case <-s.chRst:
			return 0, io.ErrUnexpectedEOF
		case <-s.chSendFinEvent:
			return 0, io.ErrClosedPipe
		default:
		}

		s.mu.Lock()
		// make sure write do not overflow the max sliding window on both side
		waitsnd := s.kcp.WaitSnd()
		if waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd) {
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
			atomic.AddUint64(&DefaultSnmp.BytesSent, uint64(len(b)))
			return len(b), nil
		}

		var timeout *time.Timer
		var c <-chan time.Time
		if !s.wd.IsZero() {
			if time.Now().After(s.wd) {
				s.mu.Unlock()
				return 0, errTimeout
			}
			delay := s.wd.Sub(time.Now())
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		select {
		case <-s.chClose:
			return 0, io.ErrClosedPipe
		case <-s.chRst:
			return 0, io.ErrUnexpectedEOF
		case <-s.chSendFinEvent:
			return 0, io.EOF
		case <-s.chWriteEvent:
			if timeout != nil {
				timeout.Stop()
			}
		case <-c:
			return 0, errTimeout
		}
	}
}

func (s *UDPStream) Dial(timeoutMs int) error {
	Logf(INFO, "UDPStream::Dial uuid:%v accepted:%v", s.uuid, s.accepted)

	if s.accepted {
		return nil
	}
	checkTime := timeoutMs/DialCheckInterval + 1
	for i := 0; i < checkTime; i++ {
		time.Sleep(time.Duration(DialCheckInterval) * time.Millisecond)
		s.mu.Lock()
		snd_una := s.kcp.snd_una
		s.mu.Unlock()
		if snd_una > 0 {
			return nil
		}
	}
	return errTimeout
}

// Close closes the connection.
func (s *UDPStream) Close() error {
	Logf(INFO, "UDPStream::Close uuid:%v accepted:%v", s.uuid, s.accepted)

	var once bool
	s.closeOnce.Do(func() {
		once = true
	})
	if !once {
		return io.ErrClosedPipe
	}

	s.WriteFlag(RST, nil)
	close(s.chClose)
	s.hrtTicker.Stop()
	atomic.AddUint64(&DefaultSnmp.CurrEstab, ^uint64(0))

	SystemTimedSched.Put(s.clean, time.Now().Add(CleanTimeout))
	return nil
}

func (s *UDPStream) CloseWrite() {
	Logf(INFO, "UDPStream::CloseWrite uuid:%v accepted:%v", s.uuid, s.accepted)

	s.sendFinOnce.Do(func() {
		s.WriteFlag(FIN, nil)
		close(s.chSendFinEvent)
	})
}

func (s *UDPStream) reset() {
	Logf(INFO, "UDPStream::reset uuid:%v accepted:%v", s.uuid, s.accepted)

	s.recvSynOnce.Do(func() {
		close(s.chRst)
		s.kcp.ReleaseTX()
	})
}

func (s *UDPStream) clean() {
	Logf(INFO, "UDPStream::clean uuid:%v accepted:%v", s.uuid, s.accepted)
	close(s.chClean)
}

// sess update to trigger protocol
func (s *UDPStream) update() {
	select {
	case <-s.hrtTicker.C:
		s.WriteFlag(HRT, nil)
	case <-s.chClean:
		s.mu.Lock()
		s.kcp.ReleaseTX()
		s.mu.Unlock()
		s.cleancb(s.uuid)
	default:
		s.mu.Lock()
		if s.kcp.state == 0xFFFFFFFF {
			s.reset()
			s.mu.Unlock()
			return
		}
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

// uncork sends data in txqueue if there is any
func (s *UDPStream) uncork() {
	s.mu.Lock()
	if len(s.msgss) == 0 {
		s.mu.Unlock()
		return
	}
	msgss := s.msgss
	s.msgss = make([][]ipv4.Message, 0)
	s.mu.Unlock()

	//todo if tunnel output failure, can change tunnel or else
	for i, msgs := range msgss {
		if len(msgs) > 0 {
			s.tunnels[i].output(msgs)
		}
	}
}

func (s *UDPStream) output(buf []byte, lostSegs, fastRetransSegs, earlyRetransSegs uint64) {
	copy(buf, s.uuid[:])
	appendCount := len(s.tunnels)
	for i := len(s.msgss); i < appendCount; i++ {
		s.msgss = append(s.msgss, make([]ipv4.Message, 0))
	}
	for i := 0; i < appendCount; i++ {
		s.msgss[i] = append(s.msgss[i], s.fillMsg(buf, s.remotes[i]))
	}
}

func (s *UDPStream) input(data []byte) error {
	Logf(DEBUG, "UDPStream:input uuid:%v accepted:%v data:%v", s.uuid, s.accepted, len(data))

	var kcpInErrors uint64

	s.mu.Lock()
	synPacket := s.kcp.rcv_nxt == 0
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

	//first packet must by syn
	if synPacket && s.accepted {
		return s.readSyn(data)
	}
	return nil
}

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
	msg.Addr = remote
	return
}

func (s *UDPStream) readSyn(data []byte) error {
	if len(data) <= FlagOffset || data[FlagOffset] != SYN {
		s.Close()
		return io.ErrClosedPipe
	}
	//must be syn
	if n, err := s.Read(nil); n != 0 || err != nil {
		s.Close()
		return io.ErrClosedPipe
	}
	return nil
}

func (s *UDPStream) cmdRead(flag byte, data []byte, b []byte) (n int, err error) {
	switch flag {
	case PSH:
		return s.recvPsh(data, b)
	case SYN:
		return s.recvSyn(data)
	case FIN:
		return s.recvFin(data)
	case HRT:
		return s.recvHrt(data)
	case RST:
		return s.recvRst(data)
	default:
		return 0, errStreamFlag
	}
}

func (s *UDPStream) recvPsh(data []byte, b []byte) (n int, err error) {
	return copy(b, data), nil
}

func (s *UDPStream) recvSyn(data []byte) (n int, err error) {
	Logf(INFO, "UDPStream::recvSyn uuid:%v accepted:%v", s.uuid, s.accepted)

	var once bool
	s.recvSynOnce.Do(func() {
		once = true
	})
	if !once {
		return len(data), nil
	}

	endpointInfo := string(data)
	remoteIps := strings.Split(endpointInfo, ":")
	if len(remoteIps) == 0 {
		return len(data), errSynInfo
	}
	tunnels, remotes := s.sel.Pick(remoteIps)
	if len(tunnels) == 0 || len(tunnels) != len(remotes) {
		return len(data), errSynInfo
	}
	s.remoteIps = remoteIps
	s.tunnels = tunnels
	s.remotes = remotes
	return len(data), nil
}

func (s *UDPStream) recvFin(data []byte) (n int, err error) {
	Logf(INFO, "UDPStream::recvFin uuid:%v accepted:%v", s.uuid, s.accepted)

	s.recvFinOnce.Do(func() {
		close(s.chRecvFinEvent)
	})
	return len(data), io.EOF
}

func (s *UDPStream) recvHrt(data []byte) (n int, err error) {
	Logf(INFO, "UDPStream::recvHrt uuid:%v accepted:%v", s.uuid, s.accepted)
	return len(data), nil
}

func (s *UDPStream) recvRst(data []byte) (n int, err error) {
	Logf(INFO, "UDPStream::recvRst uuid:%v accepted:%v", s.uuid, s.accepted)
	s.reset()
	return len(data), io.ErrUnexpectedEOF
}
