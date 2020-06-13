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
	DefaultAcceptBacklog = 128
	DefaultDialTimeout   = time.Millisecond * 200
)

var (
	errInvalidRemoteIP = errors.New("invalid remote ip")
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

type RouteSelector interface {
	Pick(remoteIps []string) (tunnels []*UDPTunnel, remotes []net.Addr)
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

var DefaultKCPOption = &KCPOption{
	nodelay:  1,
	interval: 20,
	resend:   2,
	nc:       1,
}

var DefaultTunOption = &TunnelOption{
	readBuffer:  16 * 1024 * 1024,
	writeBuffer: 16 * 1024 * 1024,
}

func netAddrToIP(addr net.Addr) string {
	switch addr := addr.(type) {
	case *net.UDPAddr:
		return addr.IP.String()
	case *net.TCPAddr:
		return addr.IP.String()
	default:
		return ""
	}
}

type UDPTransport struct {
	kcpOption  *KCPOption
	streamm    cmap.ConcurrentMap
	acceptChan chan *UDPStream

	tunnelHostM map[string]*UDPTunnel
	sel         RouteSelector

	die     chan struct{} // notify the listener has closed
	dieOnce sync.Once
}

func NewUDPTransport(sel RouteSelector, option *KCPOption, accept bool) (t *UDPTransport, err error) {
	var accepteChan chan *UDPStream
	if accept {
		accepteChan = make(chan *UDPStream, DefaultAcceptBacklog)
	}
	t = &UDPTransport{
		kcpOption:   option,
		streamm:     cmap.New(),
		acceptChan:  accepteChan,
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

	t.tunnelHostM[lAddr] = tunnel
	return tunnel, nil
}

func (t *UDPTransport) NewStream(uuid gouuid.UUID, accepted bool, remoteIps []string) (stream *UDPStream, err error) {
	Logf(INFO, "UDPTransport::NewStream uuid:%v accepted:%v remoteIps:%v", uuid, accepted, remoteIps)

	stream, err = NewUDPStream(uuid, accepted, remoteIps, t.sel, func(uuid gouuid.UUID) {
		t.handleClose(uuid)
	})
	if err != nil {
		Logf(ERROR, "UDPTransport::NewStream uuid:%v accepted:%v remoteIps:%v err:%v", uuid, accepted, remoteIps, err)
		return nil, err
	}
	if t.kcpOption != nil {
		stream.SetNoDelay(t.kcpOption.nodelay, t.kcpOption.interval, t.kcpOption.resend, t.kcpOption.nc)
	}
	return stream, err
}

//interface
func (t *UDPTransport) Open(remoteIps []string) (stream *UDPStream, err error) {
	uuid := gouuid.NewV1()
	Logf(INFO, "UDPTransport::Open uuid:%v remoteIps:%v", uuid, remoteIps)

	stream, err = t.NewStream(uuid, false, remoteIps)
	if err != nil {
		Logf(ERROR, "UDPTransport::Open uuid:%v remoteIps:%v err:%v", uuid, remoteIps, err)
		return nil, errors.WithStack(err)
	}
	t.streamm.Set(uuid.String(), stream)
	err = stream.Dial(DefaultDialTimeout)
	if err != nil {
		Logf(WARN, "UDPTransport::Open timeout uuid:%v remoteIps:%v", uuid, remoteIps)
		stream.Close()
		return nil, err
	}
	return stream, nil
}

func (t *UDPTransport) Accept() (stream *UDPStream, err error) {
	select {
	case stream = <-t.acceptChan:
		Logf(INFO, "UDPTransport::Accept uuid:%v", stream.GetUUID())
		return stream, nil
	case <-t.die:
		return nil, errors.WithStack(io.ErrClosedPipe)
	}
}

func (t *UDPTransport) handleInput(data []byte, rAddr net.Addr) {
	var uuid gouuid.UUID
	copy(uuid[:], data)

	stream, new := t.handleOpen(uuid, netAddrToIP(rAddr))
	if stream != nil {
		if stream.input(data) == nil && new && t.acceptChan != nil {
			t.acceptChan <- stream
		}
	}
}

func (t *UDPTransport) handleOpen(uuid gouuid.UUID, remoteIP string) (stream *UDPStream, new bool) {
	s, ok := t.streamm.Get(uuid.String())
	if ok {
		return s.(*UDPStream), false
	}
	s = t.streamm.Upsert(uuid.String(), nil, func(exist bool, valueInMap interface{}, newValue interface{}) interface{} {
		if exist {
			return valueInMap
		}
		s, err := t.NewStream(uuid, true, []string{remoteIP})
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
