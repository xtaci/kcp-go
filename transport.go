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

type RouteSelector interface {
	AddTunnel(tunnel *UDPTunnel)
	Pick(ips []string) (tunnels []*UDPTunnel, remotes []net.Addr)
}

type UDPTransport struct {
	streamm    cmap.ConcurrentMap
	acceptChan chan *UDPStream

	tunnelHostM map[string]*UDPTunnel
	sel         RouteSelector

	die     chan struct{} // notify the listener has closed
	dieOnce sync.Once
}

func NewUDPTransport(sel RouteSelector) (t *UDPTransport, err error) {
	t = &UDPTransport{
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
		return nil, errors.WithStack(err)
	}
	t.tunnelHostM[lAddr] = tunnel
	t.sel.AddTunnel(tunnel)
	return tunnel, nil
}

func (t *UDPTransport) NewStream(uuid gouuid.UUID, ips []string, accepted bool) (stream *UDPStream, err error) {
	stream, err = NewUDPStream(uuid, ips, t.sel, t, accepted)
	if err != nil {
		return nil, err
	}
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
func (t *UDPTransport) Open(ips []string) (stream *UDPStream, err error) {
	uuid := gouuid.NewV1()
	stream, err = t.NewStream(uuid, ips, false)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	t.streamm.Set(uuid.String(), stream)
	return stream, nil
}

//interface
func (t *UDPTransport) Accept() (stream *UDPStream, err error) {
	select {
	case stream = <-t.acceptChan:
		return stream, nil
	case <-t.die:
		return nil, errors.WithStack(io.ErrClosedPipe)
	}
}
