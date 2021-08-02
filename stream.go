package kcp

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gouuid "github.com/satori/go.uuid"
	"golang.org/x/net/ipv4"
)

var (
	errTimeout      = errors.New("err timeout")
	errTunnelPick   = errors.New("err tunnel pick")
	errStreamFlag   = errors.New("err stream flag")
	errSynInfo      = errors.New("err syn info")
	errDialParam    = errors.New("err dial param")
	errRemoteStream = errors.New("err remote stream")

	errDialVersionNotSupport = errors.New("err dial version not support")
)

const (
	CleanTimeout              = time.Second * 5
	HeartbeatInterval         = time.Second * 30
	DefaultDeadLink           = 10
	DefaultAckNoDelayRatio    = 0.7
	DefaultAckNoDelayCount    = 60
	DefaultParallelDelayMs    = 350
	DefaultParallelIntervalMs = 150
	DefaultParallelDurationMs = 60 * 1000
)

const (
	StateNone = iota // 0
	StateEstablish
	StateClosed
)

const (
	PSH = '1'
	SYN = '2'
	FIN = '3'
	HRT = '4'
	RST = '5'
)

const (
	FRAME_FLAG_PARALLEL_NTF = 0x08
	FRAME_FLAG_REPLICA      = 0x04
)

// FRAME versions
const (
	_ byte = iota
	FV1
)

const (
	_ byte = iota
	DV1
)

type clean_callback func(uuid gouuid.UUID)

type (
	// UDPStream defines a KCP session
	UDPStream struct {
		uuid     gouuid.UUID
		state    int
		sel      TunnelSelector
		kcp      *KCP // KCP ARQ protocol
		tunnels  []*UDPTunnel
		locals   []*net.UDPAddr
		remotes  []*net.UDPAddr
		accepted bool
		cleancb  clean_callback

		// kcp receiving is based on packets
		// recvbuf turns packets into stream
		recvbuf []byte
		sendbuf []byte
		bufptr  []byte

		// settings
		hrtTicker  *time.Ticker // heart beat ticker
		cleanTimer *time.Timer  // clean timer
		rd         time.Time    // read deadline
		wd         time.Time    // write deadline
		headerSize int          // the header size additional to a KCP frame
		ackNoDelay bool         // send ack immediately for each incoming packet(testing purpose)
		writeDelay bool         // delay kcp.flush() for Write() for bulk transfer

		// notifications
		recvSynOnce    sync.Once
		sendFinOnce    sync.Once
		chSendFinEvent chan struct{} // notify send fin
		recvFinOnce    sync.Once
		chRecvFinEvent chan struct{} // notify recv fin
		rstOnce        sync.Once
		chRst          chan struct{} // notify current stream reset
		closeOnce      sync.Once
		chClose        chan struct{} // notify stream has Closed
		chDialEvent    chan struct{} // notify Dial() has finished
		chReadEvent    chan struct{} // notify Read() can be called without blocking
		chWriteEvent   chan struct{} // notify Write() can be called without blocking
		chFlushImmed   chan struct{} // notify start flush timer
		chFlushDelay   chan struct{} // notify start flush timer

		// packets waiting to be sent on wire
		msgss [][]ipv4.Message
		mu    sync.Mutex

		parallelDelayMs    uint32
		parallelIntervalMs uint32
		parallelDurationMs uint32
		parallelExpireMs   uint32
		parallelDelaytsMax uint32
		primaryBreakOff    bool

		ackNoDelayRatio float32
		ackNoDelayCount uint32
	}
)

// newUDPSession create a new udp session for client or server
func NewUDPStream(uuid gouuid.UUID, accepted bool, remotes []string, sel TunnelSelector, cleancb clean_callback) (stream *UDPStream, err error) {
	tunnels := sel.Pick(remotes)
	if len(tunnels) == 0 || len(tunnels) != len(remotes) {
		return nil, errTunnelPick
	}

	remoteAddrs := make([]*net.UDPAddr, len(remotes))
	for i, remote := range remotes {
		remoteAddr, err := net.ResolveUDPAddr("udp", remote)
		if err != nil {
			return nil, err
		}
		remoteAddrs[i] = remoteAddr
	}

	locals := make([]*net.UDPAddr, len(tunnels))
	for i, tunnel := range tunnels {
		locals[i] = tunnel.LocalAddr()
	}

	stream = new(UDPStream)
	stream.chClose = make(chan struct{})
	stream.chRst = make(chan struct{})
	stream.chSendFinEvent = make(chan struct{})
	stream.chRecvFinEvent = make(chan struct{})
	stream.chDialEvent = make(chan struct{}, 1)
	stream.chReadEvent = make(chan struct{}, 1)
	stream.chWriteEvent = make(chan struct{}, 1)
	stream.chFlushImmed = make(chan struct{}, 1)
	stream.chFlushDelay = make(chan struct{}, 1)
	stream.sendbuf = make([]byte, mtuLimit)
	stream.recvbuf = make([]byte, mtuLimit)
	stream.uuid = uuid
	stream.sel = sel
	stream.cleancb = cleancb
	if uuid.Version() == gouuid.V1 {
		stream.headerSize = gouuid.Size
	} else {
		stream.headerSize = gouuid.Size + 1
	}
	stream.msgss = make([][]ipv4.Message, 0)
	stream.accepted = accepted
	stream.tunnels = tunnels
	stream.locals = locals
	stream.remotes = remoteAddrs
	stream.hrtTicker = time.NewTicker(HeartbeatInterval)
	stream.cleanTimer = time.NewTimer(CleanTimeout)
	stream.parallelDelayMs = DefaultParallelDelayMs
	stream.parallelIntervalMs = DefaultParallelIntervalMs
	stream.parallelDurationMs = DefaultParallelDurationMs
	stream.ackNoDelayRatio = DefaultAckNoDelayRatio
	stream.ackNoDelayCount = DefaultAckNoDelayCount
	stream.primaryBreakOff = true

	stream.kcp = NewKCP(1, func(buf []byte, size int, current, xmitMax, delayts uint32) {
		if size >= IKCP_OVERHEAD+stream.headerSize {
			stream.output(buf[:size], current, xmitMax, delayts)
		}
	})
	stream.kcp.ReserveBytes(stream.headerSize)
	stream.kcp.dead_link = DefaultDeadLink
	stream.kcp.cwnd = 1

	stream.cleanTimer.Stop()
	go stream.update()

	Logf(INFO, "NewUDPStream uuid:%v accepted:%v locals:%v remotes:%v", uuid, accepted, locals, remotes)
	return stream, nil
}

// LocalAddr returns the local network address. The Addr returned is shared by all invocations of LocalAddr, so do not modify it.
func (s *UDPStream) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locals[0]
}

func (s *UDPStream) LocalAddrs() []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	addrs := make([]net.Addr, len(s.locals))
	for i := 0; i < len(s.locals); i++ {
		addrs[i] = s.locals[i]
	}
	return addrs
}

// RemoteAddr returns the remote network address. The Addr returned is shared by all invocations of RemoteAddr, so do not modify it.
func (s *UDPStream) RemoteAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remotes[0]
}

func (s *UDPStream) RemoteAddrs() []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	addrs := make([]net.Addr, len(s.remotes))
	for i := 0; i < len(s.locals); i++ {
		addrs[i] = s.remotes[i]
	}
	return addrs
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

func (s *UDPStream) SetParallelDelayMs(delayMs uint32) {
	if delayMs == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parallelDelayMs = delayMs
}

func (s *UDPStream) SetParallelIntervalMs(intervalMs uint32) {
	if intervalMs == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parallelIntervalMs = intervalMs
}

func (s *UDPStream) SetParallelDurationMs(durationMs uint32) {
	if durationMs == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parallelDurationMs = durationMs
}

func (s *UDPStream) WaitSnd() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kcp.WaitSnd()
}

func (s *UDPStream) SetAckNoDelayThreshold(ackNoDelayRatio float32, ackNoDelayCount uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackNoDelayRatio = ackNoDelayRatio
	s.ackNoDelayCount = ackNoDelayCount
}

// GetConv gets conversation id of a session
func (s *UDPStream) GetConv() uint32      { return s.kcp.conv }
func (s *UDPStream) GetUUID() gouuid.UUID { return s.uuid }

// Read implements net.Conn
func (s *UDPStream) Read(b []byte) (n int, err error) {
	select {
	case <-s.chClose:
		return 0, io.ErrClosedPipe
	case <-s.chRst:
		return 0, io.ErrUnexpectedEOF
	case <-s.chRecvFinEvent:
		return 0, io.EOF
	default:
	}

	for {
		s.mu.Lock()
		for {
			if len(s.bufptr) > 0 { // copy from buffer into b, ctrl msg should not cache into this
				copyn := copy(b[n:], s.bufptr)
				s.bufptr = s.bufptr[copyn:]
				n += copyn
				atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(copyn))
				if n == len(b) {
					s.notifyFlushEvent(s.kcp.probe_ask_tell())
					s.mu.Unlock()
					return n, nil
				}
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
				copyn, err := s.cmdRead(flag, s.recvbuf[1:], b[n:])
				s.bufptr = s.recvbuf[copyn+1:]
				if flag == PSH {
					n += copyn
				}
				atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(copyn))
				if n == len(b) || err != nil {
					s.notifyFlushEvent(s.kcp.probe_ask_tell())
					s.mu.Unlock()
					return n, err
				}
			} else if n > 0 {
				s.notifyFlushEvent(s.kcp.probe_ask_tell())
				s.mu.Unlock()
				return n, nil
			} else {
				break
			}
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
	return s.WriteBuffer(PSH, b, false)
}

// Write implements net.Conn
func (s *UDPStream) WriteFlag(flag byte, b []byte) (n int, err error) {
	return s.WriteBuffer(flag, b, flag == HRT)
}

// WriteBuffers write a vector of byte slices to the underlying connection
func (s *UDPStream) WriteBuffer(flag byte, b []byte, heartbeat bool) (n int, err error) {
	select {
	case <-s.chClose:
		return 0, io.ErrClosedPipe
	case <-s.chRst:
		return 0, io.ErrUnexpectedEOF
	case <-s.chSendFinEvent:
		return 0, io.ErrClosedPipe
	default:
	}

	// start := time.Now()
	// randId := rand.Intn(10000)

	for {
		s.mu.Lock()
		// make sure write do not overflow the max sliding window on both side
		waitsnd := s.kcp.WaitSnd()
		if waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd) {
			n := len(b)
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
			immediately := waitsnd >= int(s.kcp.snd_wnd) || waitsnd >= int(s.kcp.rmt_wnd) || !s.writeDelay
			s.mu.Unlock()
			s.notifyFlushEvent(immediately)

			atomic.AddUint64(&DefaultSnmp.BytesSent, uint64(n))

			// cost := time.Since(start)
			// Logf(DEBUG, "UDPStream::Write finish uuid:%v accepted:%v randId:%v waitsnd:%v snd_wnd:%v rmt_wnd:%v snd_buf:%v snd_queue:%v cost:%v len:%v", s.uuid, s.accepted, randId, waitsnd, s.kcp.snd_wnd, s.kcp.rmt_wnd, len(s.kcp.snd_buf), len(s.kcp.snd_queue), cost, n)
			return n, nil
		} else if heartbeat {
			s.mu.Unlock()
			return len(b), nil
		}
		// Logf(DEBUG, "UDPStream::Write block uuid:%v accepted:%v randId:%v waitsnd:%v snd_wnd:%v rmt_wnd:%v snd_buf:%v snd_queue:%v", s.uuid, s.accepted, randId, waitsnd, s.kcp.snd_wnd, s.kcp.rmt_wnd, len(s.kcp.snd_buf), len(s.kcp.snd_queue))

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

// Close closes the connection.
func (s *UDPStream) Close() error {
	var once bool
	s.closeOnce.Do(func() {
		once = true
	})

	Logf(INFO, "UDPStream::Close uuid:%v accepted:%v once:%v", s.uuid, s.accepted, once)
	if !once {
		return io.ErrClosedPipe
	}

	s.WriteFlag(RST, nil)
	close(s.chClose)
	s.hrtTicker.Stop()
	s.cleanTimer.Reset(CleanTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateEstablish {
		return nil
	}
	s.state = StateClosed
	atomic.AddUint64(&DefaultSnmp.CurrEstab, ^uint64(0))
	return nil
}

func (s *UDPStream) CloseWrite() error {
	var once bool
	s.sendFinOnce.Do(func() {
		once = true
	})
	Logf(INFO, "UDPStream::CloseWrite uuid:%v accepted:%v once:%v", s.uuid, s.accepted, once)
	if !once {
		return nil
	}

	s.WriteFlag(FIN, nil)
	close(s.chSendFinEvent)
	return nil
}

func (s *UDPStream) dial(locals []string, timeout time.Duration) error {
	Logf(INFO, "UDPStream::dial uuid:%v accepted:%v locals:%v timeout:%v", s.uuid, s.accepted, locals, timeout)

	if s.accepted {
		return nil
	} else if len(locals) == 0 {
		return errDialParam
	}

	dialBuf, err := s.encodeDialInfo(locals)
	if err != nil {
		return err
	}
	s.WriteFlag(SYN, dialBuf)

	dialTimer := time.NewTimer(timeout)
	defer dialTimer.Stop()

	select {
	case <-s.chClose:
		return io.ErrClosedPipe
	case <-s.chRst:
		return io.ErrUnexpectedEOF
	case <-s.chRecvFinEvent:
		return io.EOF
	case <-s.chDialEvent:
		s.establish()
		return nil
	case <-dialTimer.C:
		atomic.AddUint64(&DefaultSnmp.DialTimeout, 1)
		return errTimeout
	}
}

func (s *UDPStream) accept() (err error) {
	Logf(INFO, "UDPStream::accept uuid:%v accepted:%v", s.uuid, s.accepted)

	select {
	case <-s.chClose:
		return io.ErrClosedPipe
	case <-s.chRst:
		return io.ErrUnexpectedEOF
	case <-s.chRecvFinEvent:
		return io.EOF
	default:
	}

	s.mu.Lock()
	size := s.kcp.PeekSize()
	if size <= 0 {
		s.mu.Unlock()
		return errRemoteStream
	}

	// resize the length of recvbuf to correspond to data size
	s.recvbuf = s.recvbuf[:size]
	s.kcp.Recv(s.recvbuf)
	flag := s.recvbuf[0]
	if flag != SYN {
		s.mu.Unlock()
		return errRemoteStream
	}
	_, err = s.recvSyn(s.recvbuf[1:])
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.flush()
	s.establish()
	return err
}

func (s *UDPStream) establish() {
	Logf(INFO, "UDPStream::establish uuid:%v accepted:%v", s.uuid, s.accepted)

	currestab := atomic.AddUint64(&DefaultSnmp.CurrEstab, 1)
	atomicSetMax(&DefaultSnmp.MaxConn, currestab)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateEstablish
}

func (s *UDPStream) reset() {
	var once bool
	s.rstOnce.Do(func() {
		once = true
	})

	Logf(INFO, "UDPStream::reset uuid:%v accepted:%v once:%v", s.uuid, s.accepted, once)
	if !once {
		return
	}

	close(s.chRst)
	s.kcp.ReleaseTX()
}

// sess update to trigger protocol
func (s *UDPStream) update() {
	var flushTimer *time.Timer
	var flushTimerCh <-chan time.Time

	for {
		select {
		case <-s.cleanTimer.C:
			Logf(INFO, "UDPStream::clean uuid:%v accepted:%v", s.uuid, s.accepted)
			s.mu.Lock()
			s.kcp.ReleaseTX()
			s.mu.Unlock()
			if flushTimer != nil {
				flushTimer.Stop()
			}
			s.cleancb(s.uuid)
			return
		case <-s.hrtTicker.C:
			Logf(DEBUG, "UDPStream::heartbeat uuid:%v accepted:%v", s.uuid, s.accepted)
			s.WriteFlag(HRT, nil)
		case <-s.chFlushDelay:
			if flushTimer == nil {
				flushTimer = time.NewTimer(time.Duration(s.kcp.interval) * time.Millisecond)
				flushTimerCh = flushTimer.C
			}
		case <-s.chFlushImmed:
			if flushTimer != nil {
				flushTimer.Stop()
			}
			if interval := s.flush(); interval != 0 {
				flushTimer = time.NewTimer(time.Duration(interval) * time.Millisecond)
				flushTimerCh = flushTimer.C
			} else {
				flushTimer = nil
				flushTimerCh = nil
			}
		case <-flushTimerCh:
			if interval := s.flush(); interval != 0 {
				flushTimer = time.NewTimer(time.Duration(interval) * time.Millisecond)
				flushTimerCh = flushTimer.C
			} else {
				flushTimer = nil
				flushTimerCh = nil
			}
		}
	}
}

// flush sends data in txqueue if there is any
// return if interval means next flush interval
func (s *UDPStream) flush() (interval uint32) {
	s.mu.Lock()
	if s.kcp.state != 0xFFFFFFFF {
		interval = s.kcp.flush(false)
		if s.kcp.state == 0xFFFFFFFF {
			s.reset()
		}
	}

	waitsnd := s.kcp.WaitSnd()
	notifyWrite := waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd)

	if len(s.msgss) == 0 {
		s.mu.Unlock()
		if notifyWrite {
			s.notifyWriteEvent()
		}
		return
	}
	msgss := s.msgss
	tunnels := s.tunnels[:len(msgss)]
	s.msgss = make([][]ipv4.Message, 0)
	s.mu.Unlock()

	if notifyWrite {
		s.notifyWriteEvent()
	}

	// Logf(DEBUG, "UDPStream::flush uuid:%v accepted:%v waitsnd:%v snd_wnd:%v rmt_wnd:%v msgss:%v notifyWrite:%v", s.uuid, s.accepted, waitsnd, s.kcp.snd_wnd, s.kcp.rmt_wnd, len(msgss), notifyWrite)

	//if tunnel output failure, can change tunnel or else ?
	for i, msgs := range msgss {
		if len(msgs) > 0 {
			tunnels[i].output(msgs)
		}
	}
	return
}

func (s *UDPStream) tryParallel(current uint32) bool {
	if current == 0 {
		current = currentMs()
	}
	var trigger bool
	if current >= s.parallelExpireMs {
		Logf(INFO, "UDPStream::tryParallel uuid:%v accepted:%v", s.uuid, s.accepted)
		atomic.AddUint64(&DefaultSnmp.Parallels, 1)
		trigger = true
		s.parallelDelaytsMax = 0
	}
	s.primaryBreakOff = true
	s.parallelExpireMs = current + s.parallelDurationMs
	return trigger
}

func (s *UDPStream) getParallel(current, xmitMax, delayts uint32) (parallel int, trigger bool) {
	if delayts >= s.parallelDelayMs {
		trigger = s.tryParallel(current)
	}
	if current >= s.parallelExpireMs && !s.primaryBreakOff {
		return 1, trigger
	}
	if delayts > s.parallelDelaytsMax {
		s.parallelDelaytsMax = delayts
	}
	parallel = 2
	if s.parallelDelaytsMax > s.parallelDelayMs {
		parallel += int((s.parallelDelaytsMax - s.parallelDelayMs) / s.parallelIntervalMs)
	}
	if parallel > len(s.tunnels) {
		parallel = len(s.tunnels)
	}
	return parallel, trigger
}

func (s *UDPStream) output(buf []byte, current, xmitMax, delayts uint32) {
	var appendCount int
	var trigger bool
	if s.parallelDelayMs == 0 || s.state == StateNone {
		appendCount = len(s.tunnels)
	} else {
		appendCount, trigger = s.getParallel(current, xmitMax, delayts)
	}
	for i := len(s.msgss); i < appendCount; i++ {
		s.msgss = append(s.msgss, make([]ipv4.Message, 0))
	}

	// Logf(DEBUG, "UDPStream::output uuid:%v accepted:%v len:%v xmitMax:%v appendCount:%v", s.uuid, s.accepted, len(buf), xmitMax, appendCount)

	msg := ipv4.Message{}
	copy(buf, s.uuid[:])
	s.encodeFrameHeader(buf[:s.headerSize], trigger)
	msg.Buffers = [][]byte{buf}
	msg.Addr = s.remotes[0]
	s.msgss[0] = append(s.msgss[0], msg)

	for i := 1; i < appendCount; i++ {
		msg := ipv4.Message{}
		bts := xmitBuf.Get().([]byte)[:len(buf)]
		copy(bts, buf)
		s.setFrameReplica(bts[:s.headerSize])
		msg.Buffers = [][]byte{bts}
		msg.Addr = s.remotes[i]
		s.msgss[i] = append(s.msgss[i], msg)
	}
}

func (s *UDPStream) input(data []byte) {
	var kcpInErrors uint64

	trigger, replica := s.decodeFrameHeader(data)

	s.mu.Lock()
	if trigger {
		s.tryParallel(currentMs())
	}
	if !replica {
		s.primaryBreakOff = false
	}
	if ret := s.kcp.Input(data[s.headerSize:], !replica, false); ret != 0 {
		kcpInErrors++
	}

	if n := s.kcp.PeekSize(); n > 0 {
		s.notifyReadEvent()
	}

	waitsnd := s.kcp.WaitSnd()
	if waitsnd < int(s.kcp.snd_wnd) && waitsnd < int(s.kcp.rmt_wnd) {
		s.notifyWriteEvent()
	}

	if !s.accepted && s.kcp.snd_una == 1 {
		s.notifyDialEvent()
	}

	acklen := len(s.kcp.acklist)
	immediately := (s.ackNoDelay && acklen > 0) || uint32(acklen) > s.ackNoDelayCount || (float32(acklen)/float32(s.kcp.snd_wnd) > s.ackNoDelayRatio)
	s.mu.Unlock()
	s.notifyFlushEvent(immediately)

	// Logf(DEBUG, "UDPStream::input uuid:%v accepted:%v len:%v rmtWnd:%v mmediately:%v", s.uuid, s.accepted, len(data), s.kcp.rmt_wnd, immediately)

	atomic.AddUint64(&DefaultSnmp.InPkts, 1)
	atomic.AddUint64(&DefaultSnmp.InBytes, uint64(len(data)))
	if kcpInErrors > 0 {
		atomic.AddUint64(&DefaultSnmp.KCPInErrors, kcpInErrors)
	}
}

func (s *UDPStream) notifyDialEvent() {
	select {
	case s.chDialEvent <- struct{}{}:
	default:
	}
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

func (s *UDPStream) notifyFlushEvent(immediately bool) {
	if immediately {
		select {
		case s.chFlushImmed <- struct{}{}:
		default:
		}
	} else {
		select {
		case s.chFlushDelay <- struct{}{}:
		default:
		}
	}
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

	remotes, err := s.decodeDialInfo(data)
	if err != nil {
		return len(data), err
	}
	if len(remotes) == 0 {
		return len(data), errSynInfo
	}
	tunnels := s.sel.Pick(remotes)
	if len(tunnels) == 0 || len(tunnels) != len(remotes) {
		return len(data), errSynInfo
	}
	remoteAddrs := make([]*net.UDPAddr, len(remotes))
	for i, remote := range remotes {
		remoteAddr, err := net.ResolveUDPAddr("udp", remote)
		if err != nil {
			return len(data), err
		}
		remoteAddrs[i] = remoteAddr
	}

	locals := make([]*net.UDPAddr, len(tunnels))
	for i, tunnel := range tunnels {
		locals[i] = tunnel.LocalAddr()
	}

	s.tunnels = tunnels
	s.locals = locals
	s.remotes = remoteAddrs

	Logf(INFO, "UDPStream::recvSyn uuid:%v accepted:%v locals:%v remotes:%v", s.uuid, s.accepted, locals, remotes)
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
	Logf(DEBUG, "UDPStream::recvHrt uuid:%v accepted:%v", s.uuid, s.accepted)
	return len(data), nil
}

func (s *UDPStream) recvRst(data []byte) (n int, err error) {
	Logf(INFO, "UDPStream::recvRst uuid:%v accepted:%v", s.uuid, s.accepted)
	s.reset()
	return len(data), io.ErrUnexpectedEOF
}

//---dial info---
//version uint8
//locals uint8 (len) + (uint8 + addr) + (uint8 + addr)...
func (s *UDPStream) encodeDialInfo(locals []string) ([]byte, error) {
	if s.uuid.Version() == gouuid.V1 {
		return []byte(strings.Join(locals, " ")), nil
	}
	addrLen := 1
	for _, local := range locals {
		_, err := net.ResolveUDPAddr("udp", local)
		if err != nil {
			return nil, err
		}
		addrLen += (1 + len(local))
	}
	buf := make([]byte, 1+addrLen)
	encodeBuf := ikcp_encode8u(buf, DV1)
	encodeBuf = ikcp_encode8u(encodeBuf, byte(len(locals)))
	for _, local := range locals {
		encodeBuf = encode8uString(encodeBuf, local)
	}
	return buf, nil
}

func (s *UDPStream) decodeDialInfo(buf []byte) ([]string, error) {
	if s.uuid.Version() == gouuid.V1 {
		return strings.Split(string(buf), " "), nil
	}
	var err error
	var version byte
	var addrs byte
	if buf, err = decode8u(buf, &version); err != nil {
		return nil, err
	}
	if version != DV1 {
		return nil, errDialVersionNotSupport
	}
	if buf, err = decode8u(buf, &addrs); err != nil {
		return nil, err
	}
	remotes := make([]string, addrs)
	for i := 0; i < int(addrs); i++ {
		if buf, err = decode8uString(buf, &remotes[i]); err != nil {
			return nil, err
		}
	}
	return remotes, nil
}

//uuid + version(4bit) + parallel_notify(1 bit) + replica(1 bit) + none_use(2 bit)
func (s *UDPStream) encodeFrameHeader(buf []byte, parallel bool) {
	copy(buf, s.uuid[:])
	if s.uuid.Version() == gouuid.V1 {
		return
	}
	verFlag := FV1 << 4
	if parallel {
		verFlag |= FRAME_FLAG_PARALLEL_NTF
	}
	buf[gouuid.Size] = verFlag
}

func (s *UDPStream) setFrameReplica(buf []byte) {
	if s.uuid.Version() == gouuid.V1 {
		return
	}
	buf[gouuid.Size] = buf[gouuid.Size] | FRAME_FLAG_REPLICA
}

func (s *UDPStream) decodeFrameHeader(buf []byte) (bool, bool) {
	if s.uuid.Version() == gouuid.V1 || len(buf) <= gouuid.Size {
		return false, false
	}
	verFlag := buf[gouuid.Size]
	if verFlag>>4 != FV1 {
		return false, false
	}
	return verFlag&FRAME_FLAG_PARALLEL_NTF != 0, verFlag&FRAME_FLAG_REPLICA != 0
}
