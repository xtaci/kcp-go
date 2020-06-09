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
		conn     net.PacketConn // the underlying packet connection
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

// Close closes the connection.
func (s *UDPTunnel) Close() error {
	Logf(INFO, "UDPTunnel::Close localAddr:%v", s.lUDPAddr)

	var once bool
	s.dieOnce.Do(func() {
		once = true
	})

	if !once {
		return io.ErrClosedPipe
	}

	// maybe leak, but that's ok
	// 1. Write before append s.txqueues
	// 2. Close
	// 3. Write append
	close(s.die)
	msgss := s.popMsgss()
	s.releaseMsgss(msgss)
	s.conn.Close()
	return nil
}

func (s *UDPTunnel) LocalIp() (ip string) {
	return s.lUDPAddr.IP.String()
}

// for test
func (s *UDPTunnel) Simulate(loss float64, delayMin, delayMax int) {
	s.loss = int(loss * 100)
	s.delayMin = delayMin
	s.delayMax = delayMax
}

func (s *UDPTunnel) pushMsgs(msgs []ipv4.Message) {
	s.mu.Lock()
	s.msgss = append(s.msgss, msgs)
	s.mu.Unlock()
	s.notifyFlush()
}

func (s *UDPTunnel) popMsgss() (msgss [][]ipv4.Message) {
	s.mu.Lock()
	msgss = s.msgss
	s.msgss = make([][]ipv4.Message, 0)
	s.mu.Unlock()
	return msgss
}

func (s *UDPTunnel) releaseMsgss(msgss [][]ipv4.Message) {
	for _, msgs := range msgss {
		for k := range msgs {
			xmitBuf.Put(msgs[k].Buffers[0])
			msgs[k].Buffers = nil
		}
	}
}

func (s *UDPTunnel) output(msgs []ipv4.Message) (err error) {
	if len(msgs) == 0 {
		return errInvalidOperation
	}

	select {
	case <-s.die:
		return io.ErrClosedPipe
	default:
	}

	if s.loss == 0 && s.delayMin == 0 && s.delayMax == 0 {
		s.pushMsgs(msgs)
		return
	}

	succMsgs := make([]ipv4.Message, 0)
	for k := range msgs {
		if lossRand.Intn(100) >= s.loss {
			succMsgs = append(succMsgs, msgs[k])
		}
	}
	for _, msg := range succMsgs {
		delay := time.Duration(s.delayMin+lossRand.Intn(s.delayMax-s.delayMin)) * time.Millisecond
		timerSender.Send(s, msg, delay)
	}
	return
}

func (s *UDPTunnel) input(data []byte, addr net.Addr) {
	s.inputcb(data, addr)
}

func (s *UDPTunnel) notifyFlush() {
	Logf(DEBUG, "UDPTunnel::notifyFlush localAddr:%v", s.lUDPAddr)

	select {
	case s.chFlush <- struct{}{}:
	default:
	}
}

func (s *UDPTunnel) notifyReadError(err error) {
	Logf(WARN, "UDPTunnel::notifyReadError localAddr:%v err:%v", s.lUDPAddr, err)
	//read错误，有可能需要直接Close
}

func (s *UDPTunnel) notifyWriteError(err error) {
	Logf(WARN, "UDPTunnel::notifyWriteError localAddr:%v err:%v", s.lUDPAddr, err)
	//得确认是目标的问题，还是自身的问题
}
