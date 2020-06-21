package kcp

import (
	"io"
	"net"
	"sync"
	"time"

	cmap "github.com/1ucio/concurrent-map"
	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
)

var (
	DefaultAcceptBacklog = 1024
	DefaultDialTimeout   = time.Millisecond * 200
)

type LogLevel int

const (
	DEBUG = LogLevel(1)
	INFO  = LogLevel(2)
	WARN  = LogLevel(3)
	ERROR = LogLevel(4)
	FATAL = LogLevel(5)
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
	nodelay  int
	interval int
	resend   int
	nc       int
}

type TunnelOption struct {
	readBuffer  int
	writeBuffer int
}

var FastKCPOption = &KCPOption{
	nodelay:  1,
	interval: 20,
	resend:   2,
	nc:       1,
}

var DefaultTunOption = &TunnelOption{
	readBuffer:  16 * 1024 * 1024,
	writeBuffer: 16 * 1024 * 1024,
}

type UDPTransport struct {
	kcpOption  *KCPOption
	streamm    cmap.ConcurrentMap
	acceptChan chan *UDPStream

	tunnelHostM map[string]*UDPTunnel
	sel         TunnelSelector

	die     chan struct{} // notify the listener has closed
	dieOnce sync.Once
}

func NewUDPTransport(sel TunnelSelector, option *KCPOption) (t *UDPTransport, err error) {
	t = &UDPTransport{
		kcpOption:   option,
		streamm:     cmap.New(),
		acceptChan:  make(chan *UDPStream, DefaultAcceptBacklog),
		tunnelHostM: make(map[string]*UDPTunnel),
		sel:         sel,
		die:         make(chan struct{}),
	}
	return t, nil
}

func (t *UDPTransport) InitTunnel(tunnel *UDPTunnel, option *TunnelOption) error {
	if option == nil {
		return nil
	}
	if err := tunnel.SetReadBuffer(option.readBuffer); err != nil {
		return err
	}
	if err := tunnel.SetWriteBuffer(option.writeBuffer); err != nil {
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

	tunnel, err = NewUDPTunnel(lAddr, func(data []byte, rAddr net.Addr) {
		t.handleInput(data, rAddr)
	})
	if err != nil {
		Logf(ERROR, "UDPTransport::NewTunnel lAddr:%v err:%v", lAddr, err)
		return nil, errors.WithStack(err)
	}
	err = t.InitTunnel(tunnel, tunOption)
	if err != nil {
		Logf(ERROR, "UDPTransport::NewTunnel InitTunnel lAddr:%v err:%v", lAddr, err)
		tunnel.Close()
		return nil, errors.WithStack(err)
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
		stream.SetNoDelay(t.kcpOption.nodelay, t.kcpOption.interval, t.kcpOption.resend, t.kcpOption.nc)
	}
	return stream, err
}

//interface
func (t *UDPTransport) Open(remotes []string) (stream *UDPStream, err error) {
	uuid := gouuid.NewV1()
	Logf(INFO, "UDPTransport::Open uuid:%v remotes:%v", uuid, remotes)

	stream, err = t.NewStream(uuid, false, remotes)
	if err != nil {
		Logf(ERROR, "UDPTransport::Open uuid:%v remotes:%v err:%v", uuid, remotes, err)
		return nil, errors.WithStack(err)
	}
	t.streamm.Set(uuid.String(), stream)
	err = stream.Dial(DefaultDialTimeout)
	if err != nil {
		Logf(WARN, "UDPTransport::Open timeout uuid:%v remotes:%v", uuid, remotes)
		stream.Close()
		return nil, err
	}
	return stream, nil
}

func (t *UDPTransport) Accept() (stream *UDPStream, err error) {
	select {
	case stream = <-t.acceptChan:
		err := stream.Accept()
		if err != nil {
			Logf(WARN, "UDPTransport::Accept uuid:%v err:%v", stream.GetUUID(), err)
			return nil, err
		}
		Logf(INFO, "UDPTransport::Accept uuid:%v", stream.GetUUID())
		return stream, nil
	case <-t.die:
		return nil, errors.WithStack(io.ErrClosedPipe)
	}
}

func (t *UDPTransport) handleInput(data []byte, rAddr net.Addr) {
	var uuid gouuid.UUID
	copy(uuid[:], data)

	stream, new := t.handleOpen(uuid, rAddr)
	if stream == nil {
		return
	}
	stream.input(data)
	if !new {
		return
	}
	select {
	case t.acceptChan <- stream:
		break
	default:
		//todo, we can just ignore the stream in the future, so client will retry syn
		stream.Close()
	}
}

func (t *UDPTransport) handleOpen(uuid gouuid.UUID, rAddr net.Addr) (stream *UDPStream, new bool) {
	s, ok := t.streamm.Get(uuid.String())
	if ok {
		return s.(*UDPStream), false
	}
	s = t.streamm.Upsert(uuid.String(), nil, func(exist bool, valueInMap interface{}, newValue interface{}) interface{} {
		if exist {
			return valueInMap
		}
		s, err := t.NewStream(uuid, true, []string{rAddr.String()})
		if err != nil {
			return nil
		}
		new = true
		return s
	})
	return s.(*UDPStream), new
}

func (t *UDPTransport) handleClose(uuid gouuid.UUID) {
	t.streamm.Remove(uuid.String())
}
