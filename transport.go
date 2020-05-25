package kcp

import (
	"io"
	"net"
	"sync"

	cmap "github.com/1ucio/concurrent-map"
	"github.com/pkg/errors"
	gouuid "github.com/satori/go.uuid"
)

const (
	// accept backlog
	acceptBacklog = 128
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

var KCPLogf = func(lvl LogLevel, f string, args ...interface{}) {}

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
	AddTunnel(tunnel *UDPTunnel)
	Pick(remoteIps []string) (tunnels []*UDPTunnel, remotes []net.Addr)
}

type KCPOption struct {
	nodelay  int
	interval int
	resend   int
	nc       int
}

var defaultKCPOption = &KCPOption{
	nodelay:  1,
	interval: 20,
	resend:   2,
	nc:       1,
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

func NewUDPTransport(sel RouteSelector, option *KCPOption) (t *UDPTransport, err error) {
	if option == nil {
		option = defaultKCPOption
	}
	t = &UDPTransport{
		kcpOption:   option,
		streamm:     cmap.New(),
		acceptChan:  make(chan *UDPStream),
		tunnelHostM: make(map[string]*UDPTunnel),
		sel:         sel,
		die:         make(chan struct{}),
	}
	return t, nil
}

func (t *UDPTransport) NewTunnel(lAddr string) (tunnel *UDPTunnel, err error) {
	tunnel, ok := t.tunnelHostM[lAddr]
	if ok {
		return tunnel, nil
	}

	tunnel, err = NewUDPTunnel(lAddr, func(data []byte, rAddr net.Addr) {
		t.tunnelInput(data, rAddr)
	})
	if err != nil {
		KCPLogf(ERROR, "UDPTransport::NewTunnel lAddr:%v err:%v", lAddr, err)
		return nil, errors.WithStack(err)
	}
	t.tunnelHostM[lAddr] = tunnel
	t.sel.AddTunnel(tunnel)

	KCPLogf(INFO, "UDPTransport::NewTunnel lAddr:%v", lAddr)
	return tunnel, nil
}

func (t *UDPTransport) NewStream(uuid gouuid.UUID, remoteIps []string, accepted bool) (stream *UDPStream, err error) {
	stream, err = NewUDPStream(uuid, remoteIps, t.sel, t, accepted)
	stream.SetNoDelay(t.kcpOption.nodelay, t.kcpOption.interval, t.kcpOption.resend, t.kcpOption.nc)
	if err != nil {
		KCPLogf(ERROR, "UDPTransport::NewStream uuid:%v accepted:%v remoteIps:%v err:%v", uuid, accepted, remoteIps, err)
		return nil, err
	}

	KCPLogf(INFO, "UDPTransport::NewStream uuid:%v accepted:%v remoteIps:%v", uuid, accepted, remoteIps)
	return stream, err
}

func (t *UDPTransport) tunnelInput(data []byte, rAddr net.Addr) {
	var uuid gouuid.UUID
	copy(uuid[:], data)
	uuidStr := uuid.String()

	stream, ok := t.streamm.Get(uuidStr)
	if !ok {
		stream = t.streamm.Upsert(uuidStr, nil, func(exist bool, valueInMap interface{}, newValue interface{}) interface{} {
			if exist {
				return valueInMap
			}
			stream, err := t.NewStream(uuid, nil, true)
			if err != nil {
				return nil
			}
			t.acceptChan <- stream
			return stream
		})
	}
	if s, ok := stream.(*UDPStream); ok && s != nil {
		s.input(data)
	}
}

//interface
func (t *UDPTransport) Open(remoteIps []string) (stream *UDPStream, err error) {
	uuid := gouuid.NewV1()
	stream, err = t.NewStream(uuid, remoteIps, false)
	if err != nil {
		KCPLogf(ERROR, "UDPTransport::Open uuid:%v remoteIps:%v err:%v", uuid, remoteIps, err)
		return nil, errors.WithStack(err)
	}
	t.streamm.Set(uuid.String(), stream)

	KCPLogf(INFO, "UDPTransport::Open uuid:%v remoteIps:%v", uuid, remoteIps)
	return stream, nil
}

//interface
func (t *UDPTransport) Accept() (stream *UDPStream, err error) {
	select {
	case stream = <-t.acceptChan:
		KCPLogf(INFO, "UDPTransport::Accept uuid:%v", stream.GetUUID())
		return stream, nil
	case <-t.die:
		return nil, errors.WithStack(io.ErrClosedPipe)
	}
}
