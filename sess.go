package kcp

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/klauspost/crc32"

	"golang.org/x/net/ipv4"
)

// Option defines extra options
type Option interface{}

// OptionWithConvId defines conversation id
type OptionWithConvId struct {
	Id uint32
}

type errTimeout struct {
	error
}

func (errTimeout) Timeout() bool   { return true }
func (errTimeout) Temporary() bool { return true }
func (errTimeout) Error() string   { return "i/o timeout" }

const (
	defaultWndSize           = 128 // default window size, in packet
	nonceSize                = 16  // magic number
	crcSize                  = 4   // 4bytes packet checksum
	cryptHeaderSize          = nonceSize + crcSize
	mtuLimit                 = 2048
	txQueueLimit             = 8192
	rxFecLimit               = 8192
	defaultKeepAliveInterval = 10 * time.Second
)

type (
	// UDPSession defines a KCP session implemented by UDP
	UDPSession struct {
		kcp               *KCP         // the core ARQ
		l                 *Listener    // point to server listener if it's a server socket
		fec               *FEC         // forward error correction
		conn              *net.UDPConn // the underlying UDP socket
		block             BlockCrypt
		local, remote     net.Addr
		rd                time.Time // read deadline
		wd                time.Time // write deadline
		sockbuff          []byte    // kcp receiving is based on packet, I turn it into stream
		die               chan struct{}
		chReadEvent       chan struct{}
		chWriteEvent      chan struct{}
		chTicker          chan time.Time
		chUDPOutput       chan []byte
		headerSize        int
		ackNoDelay        bool
		isClosed          bool
		keepAliveInterval time.Duration
		xmitBuf           sync.Pool
		mu                sync.Mutex
	}
)

// newUDPSession create a new udp session for client or server
func newUDPSession(conv uint32, dataShards, parityShards int, l *Listener, conn *net.UDPConn, remote net.Addr, block BlockCrypt) *UDPSession {
	sess := new(UDPSession)
	sess.chTicker = make(chan time.Time, 1)
	sess.chUDPOutput = make(chan []byte, txQueueLimit)
	sess.die = make(chan struct{})
	sess.local = conn.LocalAddr()
	sess.chReadEvent = make(chan struct{}, 1)
	sess.chWriteEvent = make(chan struct{}, 1)
	sess.remote = remote
	sess.conn = conn
	sess.keepAliveInterval = defaultKeepAliveInterval
	sess.l = l
	sess.block = block
	sess.fec = newFEC(rxFecLimit, dataShards, parityShards)
	sess.xmitBuf.New = func() interface{} {
		return make([]byte, mtuLimit)
	}
	// calculate header size
	if sess.block != nil {
		sess.headerSize += cryptHeaderSize
	}
	if sess.fec != nil {
		sess.headerSize += fecHeaderSizePlus2
	}

	sess.kcp = NewKCP(conv, func(buf []byte, size int) {
		if size >= IKCP_OVERHEAD {
			ext := sess.xmitBuf.Get().([]byte)[:sess.headerSize+size]
			copy(ext[sess.headerSize:], buf)
			select {
			case sess.chUDPOutput <- ext:
			case <-sess.die:
			}
		}
	})
	sess.kcp.WndSize(defaultWndSize, defaultWndSize)
	sess.kcp.SetMtu(IKCP_MTU_DEF - sess.headerSize)

	go sess.updateTask()
	go sess.outputTask()
	if l == nil { // it's a client connection
		go sess.readLoop()
	}

	if l == nil {
		atomic.AddUint64(&DefaultSnmp.ActiveOpens, 1)
	} else {
		atomic.AddUint64(&DefaultSnmp.PassiveOpens, 1)
	}
	currestab := atomic.AddUint64(&DefaultSnmp.CurrEstab, 1)
	maxconn := atomic.LoadUint64(&DefaultSnmp.MaxConn)
	if currestab > maxconn {
		atomic.CompareAndSwapUint64(&DefaultSnmp.MaxConn, maxconn, currestab)
	}

	return sess
}

// Read implements the Conn Read method.
func (s *UDPSession) Read(b []byte) (n int, err error) {
	for {
		s.mu.Lock()
		if len(s.sockbuff) > 0 { // copy from buffer
			n = copy(b, s.sockbuff)
			s.sockbuff = s.sockbuff[n:]
			s.mu.Unlock()
			return n, nil
		}

		if s.isClosed {
			s.mu.Unlock()
			return 0, errors.New("broken pipe")
		}

		if !s.rd.IsZero() {
			if time.Now().After(s.rd) { // timeout
				s.mu.Unlock()
				return 0, errTimeout{}
			}
		}

		if n := s.kcp.PeekSize(); n > 0 { // data arrived
			if len(b) >= n {
				s.kcp.Recv(b)
			} else {
				buf := make([]byte, n)
				s.kcp.Recv(buf)
				n = copy(b, buf)
				s.sockbuff = buf[n:] // store remaining bytes into sockbuff for next read
			}
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n))
			return n, nil
		}

		var timeout <-chan time.Time
		if !s.rd.IsZero() {
			delay := s.rd.Sub(time.Now())
			timeout = time.After(delay)
		}
		s.mu.Unlock()

		// wait for read event or timeout
		select {
		case <-s.chReadEvent:
		case <-timeout:
		case <-s.die:
		}
	}
}

// Write implements the Conn Write method.
func (s *UDPSession) Write(b []byte) (n int, err error) {
	for {
		s.mu.Lock()
		if s.isClosed {
			s.mu.Unlock()
			return 0, errors.New("broken pipe")
		}

		if !s.wd.IsZero() {
			if time.Now().After(s.wd) { // timeout
				s.mu.Unlock()
				return 0, errTimeout{}
			}
		}

		if s.kcp.WaitSnd() < 2*int(s.kcp.snd_wnd) {
			n = len(b)
			max := s.kcp.mss << 8
			for {
				if len(b) <= int(max) { // in most cases
					s.kcp.Send(b)
					break
				} else {
					s.kcp.Send(b[:max])
					b = b[max:]
				}
			}
			s.kcp.current = currentMs()
			s.kcp.flush()
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesSent, uint64(n))
			return n, nil
		}

		var timeout <-chan time.Time
		if !s.wd.IsZero() {
			delay := s.wd.Sub(time.Now())
			timeout = time.After(delay)
		}
		s.mu.Unlock()

		// wait for write event or timeout
		select {
		case <-s.chWriteEvent:
		case <-timeout:
		case <-s.die:
		}
	}
}

// Close closes the connection.
func (s *UDPSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed {
		return errors.New("broken pipe")
	}
	close(s.die)
	s.isClosed = true
	atomic.AddUint64(&DefaultSnmp.CurrEstab, ^uint64(0))
	if s.l == nil { // client socket close
		return s.conn.Close()
	}

	return nil
}

// LocalAddr returns the local network address. The Addr returned is shared by all invocations of LocalAddr, so do not modify it.
func (s *UDPSession) LocalAddr() net.Addr { return s.local }

// RemoteAddr returns the remote network address. The Addr returned is shared by all invocations of RemoteAddr, so do not modify it.
func (s *UDPSession) RemoteAddr() net.Addr { return s.remote }

// SetDeadline sets the deadline associated with the listener. A zero time value disables the deadline.
func (s *UDPSession) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rd = t
	s.wd = t
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wd = t
	return nil
}

// SetWindowSize set maximum window size
func (s *UDPSession) SetWindowSize(sndwnd, rcvwnd int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.WndSize(sndwnd, rcvwnd)
}

// SetMtu sets the maximum transmission unit
func (s *UDPSession) SetMtu(mtu int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.SetMtu(mtu - s.headerSize)
}

// SetStreamMode toggles the stream mode on/off
func (s *UDPSession) SetStreamMode(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enable {
		s.kcp.stream = 1
	} else {
		s.kcp.stream = 0
	}
}

// SetACKNoDelay changes ack flush option, set true to flush ack immediately,
func (s *UDPSession) SetACKNoDelay(nodelay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackNoDelay = nodelay
}

// SetNoDelay calls nodelay() of kcp
func (s *UDPSession) SetNoDelay(nodelay, interval, resend, nc int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.NoDelay(nodelay, interval, resend, nc)
}

// SetDSCP sets the 6bit DSCP field of IP header, no effect if it's accepted from Listener
func (s *UDPSession) SetDSCP(dscp int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l == nil {
		return ipv4.NewConn(s.conn).SetTOS(dscp << 2)
	}
	return nil
}

// SetReadBuffer sets the socket read buffer, no effect if it's accepted from Listener
func (s *UDPSession) SetReadBuffer(bytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l == nil {
		return s.conn.SetReadBuffer(bytes)
	}
	return nil
}

// SetWriteBuffer sets the socket write buffer, no effect if it's accepted from Listener
func (s *UDPSession) SetWriteBuffer(bytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l == nil {
		return s.conn.SetWriteBuffer(bytes)
	}
	return nil
}

// SetKeepAlive changes per-connection NAT keepalive interval; 0 to disable, default to 10s
func (s *UDPSession) SetKeepAlive(interval int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keepAliveInterval = time.Duration(interval) * time.Second
}

// writeTo wraps write method for client & listener
func (s *UDPSession) writeTo(b []byte, addr net.Addr) (int, error) {
	if s.l == nil {
		return s.conn.Write(b)
	}
	return s.conn.WriteTo(b, addr)
}

func (s *UDPSession) outputTask() {
	// offset pre-compute
	fecOffset := 0
	if s.block != nil {
		fecOffset = cryptHeaderSize
	}
	szOffset := fecOffset + fecHeaderSize

	// fec data group
	var fecGroup [][]byte
	var fecCnt int
	var fecMaxSize int
	if s.fec != nil {
		fecGroup = make([][]byte, s.fec.shardSize)
		for k := range fecGroup {
			fecGroup[k] = make([]byte, mtuLimit)
		}
	}

	// keepalive
	var lastPing time.Time
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ext := <-s.chUDPOutput:
			var ecc [][]byte
			if s.fec != nil {
				s.fec.markData(ext[fecOffset:])
				// explicit size
				binary.LittleEndian.PutUint16(ext[szOffset:], uint16(len(ext[szOffset:])))

				// copy data to fec group
				xorBytes(fecGroup[fecCnt], fecGroup[fecCnt], fecGroup[fecCnt])
				copy(fecGroup[fecCnt], ext)
				fecCnt++
				if len(ext) > fecMaxSize {
					fecMaxSize = len(ext)
				}

				//  calculate Reed-Solomon Erasure Code
				if fecCnt == s.fec.dataShards {
					ecc = s.fec.calcECC(fecGroup, szOffset, fecMaxSize)
					for k := range ecc {
						s.fec.markFEC(ecc[k][fecOffset:])
						ecc[k] = ecc[k][:fecMaxSize]
					}
					fecCnt = 0
					fecMaxSize = 0
				}
			}

			if s.block != nil {
				io.ReadFull(rand.Reader, ext[:nonceSize])
				checksum := crc32.ChecksumIEEE(ext[cryptHeaderSize:])
				binary.LittleEndian.PutUint32(ext[nonceSize:], checksum)
				s.block.Encrypt(ext, ext)

				if ecc != nil {
					for k := range ecc {
						io.ReadFull(rand.Reader, ecc[k][:nonceSize])
						checksum := crc32.ChecksumIEEE(ecc[k][cryptHeaderSize:])
						binary.LittleEndian.PutUint32(ecc[k][nonceSize:], checksum)
						s.block.Encrypt(ecc[k], ecc[k])
					}
				}
			}

			//if rand.Intn(100) < 80 {
			if n, err := s.writeTo(ext, s.remote); err == nil {
				atomic.AddUint64(&DefaultSnmp.OutSegs, 1)
				atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(n))
			}
			//}

			if ecc != nil {
				for k := range ecc {
					if n, err := s.writeTo(ecc[k], s.remote); err == nil {
						atomic.AddUint64(&DefaultSnmp.OutSegs, 1)
						atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(n))
					}
				}
			}
			xorBytes(ext, ext, ext)
			s.xmitBuf.Put(ext)
		case <-ticker.C: // NAT keep-alive
			if len(s.chUDPOutput) == 0 {
				s.mu.Lock()
				interval := s.keepAliveInterval
				s.mu.Unlock()
				if interval > 0 && time.Now().After(lastPing.Add(interval)) {
					buf := make([]byte, 2)
					io.ReadFull(rand.Reader, buf)
					rnd := int(binary.LittleEndian.Uint16(buf))
					sz := rnd%(IKCP_MTU_DEF-s.headerSize-IKCP_OVERHEAD) + s.headerSize + IKCP_OVERHEAD
					ping := make([]byte, sz)
					io.ReadFull(rand.Reader, ping)
					s.writeTo(ping, s.remote)
					lastPing = time.Now()
				}
			}
		case <-s.die:
			return
		}
	}
}

// kcp update, input loop
func (s *UDPSession) updateTask() {
	var tc <-chan time.Time
	if s.l == nil { // client
		ticker := time.NewTicker(10 * time.Millisecond)
		tc = ticker.C
		defer ticker.Stop()
	} else {
		tc = s.chTicker
	}

	for {
		select {
		case <-tc:
			s.mu.Lock()
			current := currentMs()
			s.kcp.Update(current)
			if s.kcp.WaitSnd() < 2*int(s.kcp.snd_wnd) {
				s.notifyWriteEvent()
			}
			s.mu.Unlock()
		case <-s.die:
			if s.l != nil { // has listener
				select {
				case s.l.chDeadlinks <- s.remote:
				case <-s.l.die:
				}
			}
			return
		}
	}
}

// GetConv gets conversation id of a session
func (s *UDPSession) GetConv() uint32 {
	return s.kcp.conv
}

func (s *UDPSession) notifyReadEvent() {
	select {
	case s.chReadEvent <- struct{}{}:
	default:
	}
}

func (s *UDPSession) notifyWriteEvent() {
	select {
	case s.chWriteEvent <- struct{}{}:
	default:
	}
}

func (s *UDPSession) kcpInput(data []byte) {
	current := currentMs()
	if s.fec != nil {
		f := s.fec.decode(data)
		if f.flag == typeData || f.flag == typeFEC {
			if f.flag == typeFEC {
				atomic.AddUint64(&DefaultSnmp.FECSegs, 1)
			}

			if recovers := s.fec.input(f); recovers != nil {
				s.mu.Lock()
				s.kcp.current = current
				for k := range recovers {
					sz := binary.LittleEndian.Uint16(recovers[k])
					if int(sz) <= len(recovers[k]) && sz >= 2 {
						s.kcp.Input(recovers[k][2:sz], false)
					} else {
						atomic.AddUint64(&DefaultSnmp.FECErrs, 1)
					}
				}
				s.mu.Unlock()
				atomic.AddUint64(&DefaultSnmp.FECRecovered, uint64(len(recovers)))
			}
		}
		if f.flag == typeData {
			s.mu.Lock()
			s.kcp.current = current
			s.kcp.Input(data[fecHeaderSizePlus2:], true)
			s.mu.Unlock()
		}
	} else {
		s.mu.Lock()
		s.kcp.current = current
		s.kcp.Input(data, true)
		s.mu.Unlock()
	}

	// notify reader
	s.mu.Lock()
	if n := s.kcp.PeekSize(); n > 0 {
		s.notifyReadEvent()
	}
	if s.ackNoDelay {
		s.kcp.current = current
		s.kcp.flush()
	}
	s.mu.Unlock()
	atomic.AddUint64(&DefaultSnmp.InSegs, 1)
}

func (s *UDPSession) receiver(ch chan []byte) {
	for {
		data := s.xmitBuf.Get().([]byte)[:mtuLimit]
		if n, err := s.conn.Read(data); err == nil && n >= s.headerSize+IKCP_OVERHEAD {
			select {
			case ch <- data[:n]:
			case <-s.die:
			}
		} else if err != nil {
			return
		} else {
			atomic.AddUint64(&DefaultSnmp.InErrs, 1)
		}
	}
}

// read loop for client session
func (s *UDPSession) readLoop() {
	chPacket := make(chan []byte, txQueueLimit)
	go s.receiver(chPacket)

	for {
		select {
		case data := <-chPacket:
			raw := data
			dataValid := false
			if s.block != nil {
				s.block.Decrypt(data, data)
				data = data[nonceSize:]
				checksum := crc32.ChecksumIEEE(data[crcSize:])
				if checksum == binary.LittleEndian.Uint32(data) {
					data = data[crcSize:]
					dataValid = true
				} else {
					atomic.AddUint64(&DefaultSnmp.InCsumErrors, 1)
				}
			} else if s.block == nil {
				dataValid = true
			}

			if dataValid {
				s.kcpInput(data)
			}
			xorBytes(raw, raw, raw)
			s.xmitBuf.Put(raw)
		case <-s.die:
			return
		}
	}
}

type (
	// Listener defines a server listening for connections
	Listener struct {
		block                    BlockCrypt
		dataShards, parityShards int
		fec                      *FEC // for fec init test
		conn                     *net.UDPConn
		sessions                 map[string]*UDPSession
		chAccepts                chan *UDPSession
		chDeadlinks              chan net.Addr
		headerSize               int
		die                      chan struct{}
		rxbuf                    sync.Pool
	}

	packet struct {
		from *net.UDPAddr
		data []byte
	}
)

// monitor incoming data for all connections of server
func (l *Listener) monitor() {
	chPacket := make(chan packet, txQueueLimit)
	go l.receiver(chPacket)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case p := <-chPacket:
			raw := p.data
			data := p.data
			from := p.from
			dataValid := false
			if l.block != nil {
				l.block.Decrypt(data, data)
				data = data[nonceSize:]
				checksum := crc32.ChecksumIEEE(data[crcSize:])
				if checksum == binary.LittleEndian.Uint32(data) {
					data = data[crcSize:]
					dataValid = true
				} else {
					atomic.AddUint64(&DefaultSnmp.InCsumErrors, 1)
				}
			} else if l.block == nil {
				dataValid = true
			}

			if dataValid {
				addr := from.String()
				s, ok := l.sessions[addr]
				if !ok { // new session
					var conv uint32
					convValid := false
					if l.fec != nil {
						isfec := binary.LittleEndian.Uint16(data[4:])
						if isfec == typeData {
							conv = binary.LittleEndian.Uint32(data[fecHeaderSizePlus2:])
							convValid = true
						}
					} else {
						conv = binary.LittleEndian.Uint32(data)
						convValid = true
					}

					if convValid {
						s := newUDPSession(conv, l.dataShards, l.parityShards, l, l.conn, from, l.block)
						s.kcpInput(data)
						l.sessions[addr] = s
						l.chAccepts <- s
					}
				} else {
					s.kcpInput(data)
				}
			}

			xorBytes(raw, raw, raw)
			l.rxbuf.Put(raw)
		case deadlink := <-l.chDeadlinks:
			delete(l.sessions, deadlink.String())
		case <-l.die:
			return
		case <-ticker.C:
			now := time.Now()
			for _, s := range l.sessions {
				select {
				case s.chTicker <- now:
				default:
				}
			}
		}
	}
}

func (l *Listener) receiver(ch chan packet) {
	for {
		data := l.rxbuf.Get().([]byte)[:mtuLimit]
		if n, from, err := l.conn.ReadFromUDP(data); err == nil && n >= l.headerSize+IKCP_OVERHEAD {
			ch <- packet{from, data[:n]}
		} else if err != nil {
			return
		} else {
			atomic.AddUint64(&DefaultSnmp.InErrs, 1)
		}
	}
}

// SetReadBuffer sets the socket read buffer for the Listener
func (l *Listener) SetReadBuffer(bytes int) error {
	return l.conn.SetReadBuffer(bytes)
}

// SetWriteBuffer sets the socket write buffer for the Listener
func (l *Listener) SetWriteBuffer(bytes int) error {
	return l.conn.SetWriteBuffer(bytes)
}

// SetDSCP sets the 6bit DSCP field of IP header
func (l *Listener) SetDSCP(dscp int) error {
	return ipv4.NewConn(l.conn).SetTOS(dscp << 2)
}

// Accept implements the Accept method in the Listener interface; it waits for the next call and returns a generic Conn.
func (l *Listener) Accept() (net.Conn, error) {
	return l.AcceptKCP()
}

// AcceptKCP accepts a KCP connection
func (l *Listener) AcceptKCP() (*UDPSession, error) {
	select {
	case c := <-l.chAccepts:
		return c, nil
	case <-l.die:
		return nil, errors.New("listener stopped")
	}
}

// Close stops listening on the UDP address. Already Accepted connections are not closed.
func (l *Listener) Close() error {
	close(l.die)
	return l.conn.Close()
}

// Addr returns the listener's network address, The Addr returned is shared by all invocations of Addr, so do not modify it.
func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// Listen listens for incoming KCP packets addressed to the local address laddr on the network "udp",
func Listen(laddr string) (*Listener, error) {
	return ListenWithOptions(laddr, nil, 0, 0)
}

// ListenWithOptions listens for incoming KCP packets addressed to the local address laddr on the network "udp" with packet encryption,
// dataShards, parityShards defines Reed-Solomon Erasure Coding parameters
func ListenWithOptions(laddr string, block BlockCrypt, dataShards, parityShards int) (*Listener, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", laddr)
	if err != nil {
		return nil, errors.Wrap(err, "net.ResolveUDPAddr")
	}
	conn, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		return nil, errors.Wrap(err, "net.ListenUDP")
	}

	l := new(Listener)
	l.conn = conn
	l.sessions = make(map[string]*UDPSession)
	l.chAccepts = make(chan *UDPSession, 1024)
	l.chDeadlinks = make(chan net.Addr, 1024)
	l.die = make(chan struct{})
	l.dataShards = dataShards
	l.parityShards = parityShards
	l.block = block
	l.fec = newFEC(rxFecLimit, dataShards, parityShards)
	l.rxbuf.New = func() interface{} {
		return make([]byte, mtuLimit)
	}

	// calculate header size
	if l.block != nil {
		l.headerSize += cryptHeaderSize
	}
	if l.fec != nil {
		l.headerSize += fecHeaderSizePlus2
	}

	go l.monitor()
	return l, nil
}

// Dial connects to the remote address "raddr" on the network "udp"
func Dial(raddr string) (*UDPSession, error) {
	return DialWithOptions(raddr, nil, 0, 0)
}

// DialWithOptions connects to the remote address "raddr" on the network "udp" with packet encryption
func DialWithOptions(raddr string, block BlockCrypt, dataShards, parityShards int, opts ...Option) (*UDPSession, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, errors.Wrap(err, "net.ResolveUDPAddr")
	}

	udpconn, err := net.DialUDP("udp", nil, udpaddr)
	if err != nil {
		return nil, errors.Wrap(err, "net.DialUDP")
	}

	buf := make([]byte, 4)
	io.ReadFull(rand.Reader, buf)
	convid := binary.LittleEndian.Uint32(buf)
	for k := range opts {
		switch opt := opts[k].(type) {
		case OptionWithConvId:
			convid = opt.Id
		default:
			return nil, errors.New("unrecognized option")
		}
	}
	return newUDPSession(convid, dataShards, parityShards, nil, udpconn, udpaddr, block), nil
}

func currentMs() uint32 {
	return uint32(time.Now().UnixNano() / int64(time.Millisecond))
}
