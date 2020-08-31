package kcp

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	gouuid "github.com/satori/go.uuid"
)

var (
	DefaultAcceptBacklog   = 512
	DefaultDialTimeout     = time.Millisecond * 500
	DefaultInputQueue      = 128
	DefaultTunnelProcessor = 5
	DefaultInputTime       = 3
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

var Logf = func(lvl LogLevel, f string, args ...interface{}) {}

func (l LogLevel) String() string {
	switch l {
	case 1:
		return "DEBUG"
	case 2:
		return "INFO"
	case 3:
		return "WARNING"
	case 4:
		return "ERROR"
	case 5:
		return "FATAL"
	}
	panic("invalid LogLevel")
}

type TunnelSelector interface {
	Add(tunnel *UDPTunnel)
	Pick(remotes []string) (tunnels []*UDPTunnel)
}

type KCPOption struct {
	Nodelay  int
	Interval int
	Resend   int
	Nc       int
}

type TunnelOption struct {
	ReadBuffer  int
	WriteBuffer int
}

var FastKCPOption = &KCPOption{
	Nodelay:  1,
	Interval: 20,
	Resend:   2,
	Nc:       1,
}

var DefaultTunOption = &TunnelOption{
	ReadBuffer:  4 * 1024 * 1024,
	WriteBuffer: 4 * 1024 * 1024,
}

type inputMsg struct {
	data []byte
	addr net.Addr
}

type inputTest struct {
	t    time.Time
	addr net.Addr
}

type UDPTransport struct {
	kcpOption     *KCPOption
	streamm       ConcurrentMap
	startAccept   int32
	preAcceptChan chan chan *UDPStream
	tunnelHostM   map[string]*UDPTunnel
	sel           TunnelSelector
	die           chan struct{} // notify the listener has closed
	dieOnce       sync.Once
	inputQueues   []chan *inputMsg
}

func NewUDPTransport(sel TunnelSelector, option *KCPOption) (t *UDPTransport, err error) {
	t = &UDPTransport{
		kcpOption:     option,
		streamm:       NewConcurrentMap(),
		preAcceptChan: make(chan chan *UDPStream, DefaultAcceptBacklog),
		tunnelHostM:   make(map[string]*UDPTunnel),
		sel:           sel,
		die:           make(chan struct{}),
		inputQueues:   make([]chan *inputMsg, 0),
	}
	return t, nil
}

func (t *UDPTransport) InitTunnel(tunnel *UDPTunnel, option *TunnelOption) error {
	if option == nil {
		return nil
	}
	if err := tunnel.SetReadBuffer(option.ReadBuffer); err != nil {
		return err
	}
	if err := tunnel.SetWriteBuffer(option.WriteBuffer); err != nil {
		return err
	}
	return nil
}

func (t *UDPTransport) NewTunnel(lAddr string, tunOption *TunnelOption) (tunnel *UDPTunnel, err error) {
	Logf(INFO, "UDPTransport::NewTunnel lAddr:%v", lAddr)

	tunnel, ok := t.tunnelHostM[lAddr]
	if ok {
		return tunnel, nil
	}

	tunnelIdx := len(t.inputQueues)
	for i := 0; i < DefaultTunnelProcessor; i++ {
		t.inputQueues = append(t.inputQueues, make(chan *inputMsg, DefaultInputQueue))
		go t.processInput(tunnelIdx + i)
	}

	inputPoll := 0
	tunnel, err = NewUDPTunnel(lAddr, func(tun *UDPTunnel, data []byte, addr net.Addr) {
		msg := &inputMsg{data: data, addr: addr}
		for i := 0; i < DefaultInputTime-1; i++ {
			idx := inputPoll%DefaultTunnelProcessor + tunnelIdx
			inputPoll++
			select {
			case t.inputQueues[idx] <- msg:
				return
			default:
			}
		}
		t.inputQueues[inputPoll%DefaultTunnelProcessor+tunnelIdx] <- msg
	})

	if err != nil {
		Logf(ERROR, "UDPTransport::NewTunnel lAddr:%v err:%v", lAddr, err)
		return nil, err
	}
	err = t.InitTunnel(tunnel, tunOption)
	if err != nil {
		Logf(ERROR, "UDPTransport::NewTunnel InitTunnel lAddr:%v err:%v", lAddr, err)
		tunnel.Close()
		return nil, err
	}

	t.sel.Add(tunnel)
	t.tunnelHostM[lAddr] = tunnel
	return tunnel, nil
}

func (t *UDPTransport) NewStream(uuid gouuid.UUID, accepted bool, remotes []string) (stream *UDPStream, err error) {
	Logf(INFO, "UDPTransport::NewStream uuid:%v accepted:%v remotes:%v", uuid, accepted, remotes)

	stream, err = NewUDPStream(uuid, accepted, remotes, t.sel, func(uuid gouuid.UUID) {
		t.handleClose(uuid)
	})
	if err != nil {
		Logf(ERROR, "UDPTransport::NewStream uuid:%v accepted:%v remotes:%v err:%v", uuid, accepted, remotes, err)
		return nil, err
	}
	if t.kcpOption != nil {
		stream.SetNoDelay(t.kcpOption.Nodelay, t.kcpOption.Interval, t.kcpOption.Resend, t.kcpOption.Nc)
	}
	return stream, err
}

//interface
func (t *UDPTransport) Open(locals, remotes []string) (stream *UDPStream, err error) {
	return t.OpenTimeout(locals, remotes, DefaultDialTimeout)
}

func (t *UDPTransport) OpenTimeout(locals, remotes []string, timeout time.Duration) (stream *UDPStream, err error) {
	Logf(INFO, "UDPTransport::Open locals:%v remotes:%v timeout:%v", locals, remotes, timeout)

	uuid, err := gouuid.NewV1()
	if err != nil {
		Logf(ERROR, "UDPTransport::Open NewV1 failed. locals:%v remotes:%v err:%v", locals, remotes, err)
		return nil, err
	}

	stream, err = t.NewStream(uuid, false, remotes)
	if err != nil {
		Logf(ERROR, "UDPTransport::Open NewStream failed. uuid:%v locals:%v remotes:%v err:%v", uuid, locals, remotes, err)
		return nil, err
	}
	t.streamm.Set(uuid, stream)
	if timeout == 0 {
		timeout = DefaultDialTimeout
	}
	err = stream.dial(locals, timeout)
	if err != nil {
		Logf(WARN, "UDPTransport::Open dial timeout. uuid:%v locals:%v remotes:%v err:%v", uuid, locals, remotes, err)
		stream.Close()
		return nil, err
	}
	return stream, nil
}

func (t *UDPTransport) Accept() (*UDPStream, error) {
	atomic.StoreInt32(&t.startAccept, 1)
	for {
		select {
		case acceptChan := <-t.preAcceptChan:
			stream := <-acceptChan
			if stream != nil {
				Logf(INFO, "UDPTransport::Accept uuid:%v", stream.GetUUID())
				return stream, nil
			}
		case <-t.die:
			return nil, io.ErrClosedPipe
		}
	}
}

func (t *UDPTransport) processInput(queue int) {
	for {
		msg := <-t.inputQueues[queue]
		t.handleInput(msg.data, msg.addr)
		xmitBuf.Put(msg.data)
	}
}

func (t *UDPTransport) handleInput(data []byte, rAddr net.Addr) {
	var uuid gouuid.UUID
	copy(uuid[:], data)

	s, ok := t.streamm.Get(uuid)
	if ok {
		s.(*UDPStream).input(data)
		return
	}
	if atomic.LoadInt32(&t.startAccept) == 0 {
		return
	}

	acceptChan := make(chan *UDPStream, 1)
	select {
	case t.preAcceptChan <- acceptChan:
		break
	default:
		return
	}
	stream := t.handleOpen(uuid, []string{rAddr.String()}, data)
	acceptChan <- stream
}

func (t *UDPTransport) handleOpen(uuid gouuid.UUID, remotes []string, data []byte) *UDPStream {
	// start := time.Now()
	// defer Logf(INFO, "UDPTransport::handleOpen cost uuid:%v remotes:%v cost:%v", uuid, remotes, time.Since(start))
	Logf(INFO, "UDPTransport::handleOpen start uuid:%v remotes:%v", uuid, remotes)

	var stream *UDPStream
	t.streamm.SetIfAbsent(uuid, func() (interface{}, bool) {
		s, err := t.NewStream(uuid, true, remotes)
		if err != nil {
			return nil, false
		}
		stream = s
		return stream, true
	})
	// ignore conflict stream
	if stream != nil {
		stream.input(data)
		if err := stream.accept(); err != nil {
			Logf(INFO, "UDPTransport::handleOpen failed. uuid:%v err:%v", stream.GetUUID(), err)
			stream.Close()
			return nil
		}
	}
	return stream
}

func (t *UDPTransport) handleClose(uuid gouuid.UUID) {
	t.streamm.Remove(uuid)
}
