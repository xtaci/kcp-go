package kcp

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	errInvalidOperation = errors.New("invalid operation")
)

type input_callback func(data []byte, addr net.Addr)

type (
	// UDPTunnel defines a session implemented by UDP
	UDPTunnel struct {
		conn     *net.UDPConn // the underlying packet connection
		lUDPAddr *net.UDPAddr
		mu       sync.Mutex
		inputcb  input_callback

		// notifications
		die     chan struct{} // notify tunnel has Closed
		dieOnce sync.Once

		chFlush chan struct{} // notify Write

		// packets waiting to be sent on wire
		msgss           [][]ipv4.Message
		xconn           batchConn // for x/net
		xconnWriteError error

		//simulate
		loss     int
		delayMin int
		delayMax int
	}
)

// newUDPSession create a new udp session for client or server
func NewUDPTunnel(laddr string, inputcb input_callback) (tunnel *UDPTunnel, err error) {
	// network type detection
	lUDPAddr, err := net.ResolveUDPAddr("udp", laddr)
	if err != nil {
		return nil, err
	}
	network := "udp4"
	if lUDPAddr.IP.To4() == nil {
		network = "udp"
	}

	conn, err := net.ListenUDP(network, lUDPAddr)
	if err != nil {
		return nil, err
	}

	tunnel = new(UDPTunnel)
	tunnel.conn = conn
	tunnel.inputcb = inputcb
	tunnel.lUDPAddr = lUDPAddr
	tunnel.die = make(chan struct{})
	tunnel.chFlush = make(chan struct{}, 1)

	// cast to writebatch conn
	if lUDPAddr.IP.To4() != nil {
		tunnel.xconn = ipv4.NewPacketConn(conn)
	} else {
		tunnel.xconn = ipv6.NewPacketConn(conn)
	}

	go tunnel.readLoop()
	go tunnel.writeLoop()

	Logf(INFO, "NewUDPTunnel localAddr:%v", lUDPAddr)
	return tunnel, nil
}

func (t *UDPTunnel) SetReadBuffer(bytes int) error {
	return t.conn.SetReadBuffer(bytes)
}

func (t *UDPTunnel) SetWriteBuffer(bytes int) error {
	return t.conn.SetWriteBuffer(bytes)
}

func (t *UDPTunnel) Close() error {
	Logf(INFO, "UDPTunnel::Close localAddr:%v", t.lUDPAddr)

	var once bool
	t.dieOnce.Do(func() {
		once = true
	})

	if !once {
		return io.ErrClosedPipe
	}

	// maybe leak, but that's ok
	// 1. before pushMsgs
	// 2. Close
	// 3. pushMsgs
	close(t.die)
	msgss := t.popMsgss()
	t.releaseMsgss(msgss)
	t.conn.Close()
	return nil
}

func (t *UDPTunnel) LocalIp() (ip string) {
	return t.lUDPAddr.IP.String()
}

// for test
func (t *UDPTunnel) Simulate(loss float64, delayMin, delayMax int) {
	Logf(INFO, "UDPTunnel::Simulate localAddr:%v loss:%v delayMin:%v delayMax:%v", t.lUDPAddr, loss, delayMin, delayMax)

	t.loss = int(loss * 100)
	t.delayMin = delayMin
	t.delayMax = delayMax
}

func (t *UDPTunnel) pushMsgs(msgs []ipv4.Message) {
	t.mu.Lock()
	t.msgss = append(t.msgss, msgs)
	t.mu.Unlock()
	t.notifyFlush()
}

func (t *UDPTunnel) popMsgss() (msgss [][]ipv4.Message) {
	t.mu.Lock()
	msgss = t.msgss
	t.msgss = make([][]ipv4.Message, 0)
	t.mu.Unlock()
	return msgss
}

func (t *UDPTunnel) releaseMsgss(msgss [][]ipv4.Message) {
	for _, msgs := range msgss {
		for k := range msgs {
			xmitBuf.Put(msgs[k].Buffers[0])
			msgs[k].Buffers = nil
		}
	}
}

func (t *UDPTunnel) output(msgs []ipv4.Message) (err error) {
	if len(msgs) == 0 {
		return errInvalidOperation
	}

	select {
	case <-t.die:
		return io.ErrClosedPipe
	default:
	}

	if t.loss == 0 && t.delayMin == 0 && t.delayMax == 0 {
		t.pushMsgs(msgs)
		return
	}

	succMsgs := make([]ipv4.Message, 0)
	for k := range msgs {
		if lossRand.Intn(100) >= t.loss {
			succMsgs = append(succMsgs, msgs[k])
		}
	}
	for _, msg := range succMsgs {
		delay := time.Duration(t.delayMin+lossRand.Intn(t.delayMax-t.delayMin)) * time.Millisecond
		timerSender.Send(t, msg, delay)
	}
	return
}

func (t *UDPTunnel) input(data []byte, addr net.Addr) {
	t.inputcb(data, addr)
}

func (t *UDPTunnel) notifyFlush() {
	select {
	case t.chFlush <- struct{}{}:
	default:
	}
}

func (t *UDPTunnel) notifyReadError(err error) {
	Logf(WARN, "UDPTunnel::notifyReadError localAddr:%v err:%v", t.lUDPAddr, err)
}

func (t *UDPTunnel) notifyWriteError(err error) {
	Logf(WARN, "UDPTunnel::notifyWriteError localAddr:%v err:%v", t.lUDPAddr, err)
}
