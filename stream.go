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
	FlagOffset        = gouuid.Size + IKCP_OVERHEAD
	CleanTimeout      = time.Second * 5
	HeartbeatInterval = time.Second * 30
)

var (
	DefaultParallelXmit = 3
	DefaultParallelTime = time.Second * 60
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
		chFlushEvent   chan struct{} // notify start flush timer

		// packets waiting to be sent on wire
		msgss [][]ipv4.Message
		mu    sync.Mutex

		parallelXmit   uint32
		parallelTime   time.Duration
		parallelExpire time.Time

		pc *parallelCtrl
		hp *hostParallel
	}
)

// newUDPSession create a new udp session for client or server
func NewUDPStream(uuid gouuid.UUID, accepted bool, remotes []string, pc *parallelCtrl, sel TunnelSelector, cleancb clean_callback) (stream *UDPStream, err error) {
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
	stream.chFlushEvent = make(chan struct{}, 1)
	stream.sendbuf = make([]byte, mtuLimit)
	stream.recvbuf = make([]byte, mtuLimit)
	stream.uuid = uuid
	stream.sel = sel
	stream.cleancb = cleancb
	stream.headerSize = gouuid.Size
	stream.msgss = make([][]ipv4.Message, 0)
	stream.accepted = accepted
	stream.tunnels = tunnels
	stream.locals = locals
	stream.remotes = remoteAddrs
	stream.hrtTicker = time.NewTicker(HeartbeatInterval)
	stream.cleanTimer = time.NewTimer(CleanTimeout)
	stream.parallelXmit = uint32(DefaultParallelXmit)
	stream.parallelTime = DefaultParallelTime
	stream.pc = pc

	stream.kcp = NewKCP(1, func(buf []byte, size int, xmitMax uint32) {
		if size >= IKCP_OVERHEAD+stream.headerSize {
			stream.output(buf[:size], xmitMax)
		}
	})
	stream.kcp.ReserveBytes(stream.headerSize)

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

func (s *UDPStream) SetParallelXmit(parallelXmit uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parallelXmit = parallelXmit
}

func (s *UDPStream) SetParallelTime(parallelTime time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parallelTime = parallelTime
}

func (s *UDPStream) WaitSnd() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kcp.WaitSnd()
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
			s.bufptr = s.recvbuf[n+1:]
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n))
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
			needFlush := waitsnd >= int(s.kcp.snd_wnd) || waitsnd >= int(s.kcp.rmt_wnd) || !s.writeDelay
			s.mu.Unlock()
			if needFlush {
				s.flush()
			}
			//notify flush timer
			s.notifyFlushEvent()

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
	if s.hp != nil {
		s.hp.dec()
	}
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

	s.parallelExpire = time.Now().Add(timeout)

	s.WriteFlag(SYN, []byte(strings.Join(locals, " ")))

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

	s.parallelExpire = time.Now().Add(DefaultDialTimeout)

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
	maxconn := atomic.LoadUint64(&DefaultSnmp.MaxConn)
	if currestab > maxconn {
		atomic.CompareAndSwapUint64(&DefaultSnmp.MaxConn, maxconn, currestab)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.parallelExpire = time.Time{}

	if s.pc != nil {
		s.hp = s.pc.getHostParallel(s.remotes[0].IP.String())
		s.hp.inc()
	}
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
	var flushTimer <-chan time.Time

	for {
		select {
		case <-s.cleanTimer.C:
			Logf(INFO, "UDPStream::clean uuid:%v accepted:%v", s.uuid, s.accepted)
			s.mu.Lock()
			s.kcp.ReleaseTX()
			s.mu.Unlock()
			s.cleancb(s.uuid)
			return
		case <-s.hrtTicker.C:
			Logf(DEBUG, "UDPStream::heartbeat uuid:%v accepted:%v", s.uuid, s.accepted)
			s.WriteFlag(HRT, nil)
		case <-s.chFlushEvent:
			if flushTimer == nil {
				flushTimer = time.NewTimer(time.Duration(s.kcp.interval) * time.Millisecond).C
			}
		case <-flushTimer:
			if interval := s.flush(); interval != 0 {
				flushTimer = time.NewTimer(time.Duration(interval) * time.Millisecond).C
			} else {
				flushTimer = nil
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

func (s *UDPStream) parallelTun(xmitMax uint32) (parallel int) {
	if s.parallelXmit == 0 {
		return len(s.tunnels)
	} else if s.hp != nil && s.hp.isParallel() {
		return len(s.tunnels)
	} else if xmitMax >= s.parallelXmit && s.parallelExpire.IsZero() {
		Logf(INFO, "UDPStream::parallelTun enter uuid:%v accepted:%v parallelXmit:%v xmitMax:%v", s.uuid, s.accepted, s.parallelXmit, xmitMax)
		s.parallelExpire = time.Now().Add(s.parallelTime)
		atomic.AddUint64(&DefaultSnmp.Parallels, 1)
		if s.hp != nil {
			s.hp.incParallel()
		}
		return len(s.tunnels)
	} else if s.parallelExpire.IsZero() {
		return 1
	} else if s.parallelExpire.After(time.Now()) {
		return len(s.tunnels)
	} else {
		Logf(INFO, "UDPStream::parallelTun leave uuid:%v accepted:%v parallelXmit:%v", s.uuid, s.accepted, s.parallelXmit)
		s.parallelExpire = time.Time{}
		return 1
	}
}

func (s *UDPStream) output(buf []byte, xmitMax uint32) {
	// Logf(DEBUG, "UDPStream::output uuid:%v accepted:%v len:%v xmitMax:%v", s.uuid, s.accepted, len(buf), xmitMax)

	appendCount := s.parallelTun(xmitMax)
	for i := len(s.msgss); i < appendCount; i++ {
		s.msgss = append(s.msgss, make([]ipv4.Message, 0))
	}

	msg := ipv4.Message{}
	copy(buf, s.uuid[:])
	msg.Buffers = [][]byte{buf}
	msg.Addr = s.remotes[0]
	s.msgss[0] = append(s.msgss[0], msg)

	for i := 1; i < appendCount; i++ {
		msg := ipv4.Message{}
		bts := xmitBuf.Get().([]byte)[:len(buf)]
		copy(bts, buf)
		msg.Buffers = [][]byte{bts}
		msg.Addr = s.remotes[i]
		s.msgss[i] = append(s.msgss[i], msg)
	}
}

func (s *UDPStream) input(data []byte) {
	// Logf(DEBUG, "UDPStream::input uuid:%v accepted:%v len:%v", s.uuid, s.accepted, len(data))

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

	if !s.accepted && s.kcp.snd_una == 1 {
		s.notifyDialEvent()
	}
	s.mu.Unlock()

	s.notifyFlushEvent()

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

func (s *UDPStream) notifyFlushEvent() {
	select {
	case s.chFlushEvent <- struct{}{}:
	default:
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

	endpointInfo := string(data)
	remotes := strings.Split(endpointInfo, " ")
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
