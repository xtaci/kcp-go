package kcp

import (
	"io"
	"net"
	"sync"
	"sync/atomic"

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

type UDPTunnelSel struct {
	tunnels []*UDPTunnel
	idx     uint32
}

func (sel *UDPTunnelSel) PickTunnel() (tunnel *UDPTunnel, err error) {
	idx := atomic.AddUint32(&sel.idx, 1) % uint32(len(sel.tunnels))
	return sel.tunnels[idx], nil
}

func (sel *UDPTunnelSel) AddTunnel(tunnel *UDPTunnel) {
	sel.tunnels = append(sel.tunnels, tunnel)
}

type UDPAddrSel struct {
	addrs []*net.UDPAddr
	addrm map[string]struct{}
	idx   uint32
	mu    sync.RWMutex
}

func (sel *UDPAddrSel) PickAddr() (addr *net.UDPAddr, err error) {
	sel.mu.RLock()
	defer sel.mu.RUnlock()
	idx := atomic.AddUint32(&sel.idx, 1) % uint32(len(sel.addrs))
	return sel.addrs[idx], nil
}

func (sel *UDPAddrSel) AddAddr(newAddr *net.UDPAddr) {
	sel.mu.RLock()
	_, ok := sel.addrm[newAddr.String()]
	sel.mu.RUnlock()

	if !ok {
		sel.mu.Lock()
		_, ok = sel.addrm[newAddr.String()]
		if !ok {
			sel.addrs = append(sel.addrs, newAddr)
			sel.addrm[newAddr.String()] = struct{}{}
		}
		sel.mu.Unlock()
	}
}

type UDPTransport struct {
	streams    cmap.ConcurrentMap
	streamChan chan *UDPStream

	sel       *UDPTunnelSel
	backupSel *UDPTunnelSel
	tunnelM   map[string]struct{}

	addrSelM map[string]*UDPAddrSel
	addrMu   sync.RWMutex

	die     chan struct{} // notify the listener has closed
	dieOnce sync.Once
}

func NewUDPTransport() (t *UDPTransport, err error) {
	t = &UDPTransport{
		streams:    make(map[gouuid.UUID]*UDPStream),
		streamChan: make(chan *UDPStream),
		sel:        &UDPTunnelSel{},
		backupSel:  &UDPTunnelSel{},
		tunnelM:    make(map[string]struct{}),
		addrSelM:   make(map[string]*UDPAddrSel),
		die:        make(chan struct{}),
	}
	return t, nil
}

func (t *UDPTransport) newUDPTunnel(lAddr string, backup bool) (err error) {
	if _, ok := t.tunnelM[lAddr]; ok {
		return nil
	}
	tunnel, err := NewUDPTunnel(lAddr, func(data []byte, rAddr net.Addr) {
		t.tunnelInput(data, rAddr, backup)
	})
	if err != nil {
		return err
	}
	if !backup {
		t.sel.AddTunnel(tunnel)
	} else {
		t.backupSel.AddTunnel(tunnel)
	}
	t.tunnelM[lAddr] = struct{}{}
	return nil
}

//interface
func (t *UDPTransport) Dial(lAddr, rAddr string, backup bool) (err error) {
	rUDPAddr, err := net.ResolveUDPAddr("udp", rAddr)
	if err != nil {
		return errors.WithStack(err)
	}
	err = t.newUDPTunnel(lAddr, backup)
	if err != nil {
		return errors.WithStack(err)
	}
	t.storeRemoteAddr(rUDPAddr)
	return nil
}

//interface
func (t *UDPTransport) Listen(lAddr string, backup bool) (err error) {
	err = t.newUDPTunnel(lAddr, backup)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (t *UDPTransport) storeRemoteAddr(rUDPAddr *net.UDPAddr) {
	t.addrMu.RLock()
	addrSel, ok := t.addrSelM[rUDPAddr.IP.String()]
	t.addrMu.Unlock()

	if !ok {
		t.addrMu.Lock()
		addrSel, ok = t.addrSelM[rUDPAddr.IP.String()]
		if !ok {
			addrSel = &UDPAddrSel{}
			t.addrSelM[rUDPAddr.IP.String()] = addrSel
		}
		t.addrMu.Unlock()
	}
	addrSel.AddAddr(rUDPAddr)
}

func (t *UDPTransport) tunnelInput(data []byte, rAddr net.Addr, backup bool) {
	rUDPAddr := rAddr.(*net.UDPAddr)
	if rUDPAddr == nil {
		return
	}
	var uuid gouuid.UUID
	copy(uuid[:], data)
	stream, err := t.connected(gouuid.UUID(uuid))
	if err != nil {
		return
	}
	stream.input(data)
	t.storeRemoteAddr(rUDPAddr)
}

func (t *UDPTransport) connected(uuid gouuid.UUID) (stream *UDPStream, err error) {

}

//interface
func (t *UDPTransport) Open(ip, backupip string) (stream *UDPStream, err error) {
	addrSel, ok := t.addrSelM[ip]
	if !ok {
		return nil, errors.WithStack(errInvalidRemoteIP)
	}
	backupAddrSel, ok := t.addrSelM[backupip]
	if !ok {
		return nil, errors.WithStack(errInvalidRemoteIP)
	}

	tunnel, err := t.sel.PickTunnel()
	if err != nil {
		return nil, err
	}
	backupTunnel, err := t.backupSel.PickTunnel()
	if err != nil {
		return nil, err
	}

	remote, err := addrSel.PickAddr()
	if err != nil {
		return nil, err
	}
	backupRemote, err := backupAddrSel.PickAddr()
	if err != nil {
		return nil, err
	}

	uuid := gouuid.NewV1()
	stream, err = NewUDPStream(uuid, tunnel, backupTunnel, remote, backupRemote)
	if err != nil {
		return nil, err
	}

	t.streams.Set(string(uuid), stream)

	t.streamLock.Lock()
	t.streams[uuid] = stream
	t.streamLock.Unlock()
	return stream, err
}

//interface
func (t *UDPTransport) Accept() (stream *UDPStream, err error) {
	select {
	case stream = <-t.streamChan:
		return stream, nil
	case <-t.die:
		return nil, errors.WithStack(io.ErrClosedPipe)
	}
}
