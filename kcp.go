package kcp

const (
	IKCP_LOG_OUTPUT    = 1
	IKCP_LOG_INPUT     = 2
	IKCP_LOG_SEND      = 4
	IKCP_LOG_RECV      = 8
	IKCP_LOG_IN_DATA   = 16
	IKCP_LOG_IN_ACK    = 32
	IKCP_LOG_IN_PROBE  = 64
	IKCP_LOG_IN_WIN    = 128
	IKCP_LOG_OUT_DATA  = 256
	IKCP_LOG_OUT_ACK   = 512
	IKCP_LOG_OUT_PROBE = 1024
	IKCP_LOG_OUT_WINS  = 2048
)

const (
	IKCP_RTO_NDL     = 30  // no delay min rto
	IKCP_RTO_MIN     = 100 // normal min rto
	IKCP_RTO_DEF     = 200
	IKCP_RTO_MAX     = 60000
	IKCP_CMD_PUSH    = 81 // cmd: push data
	IKCP_CMD_ACK     = 82 // cmd: ack
	IKCP_CMD_WASK    = 83 // cmd: window probe (ask)
	IKCP_CMD_WINS    = 84 // cmd: window size (tell)
	IKCP_ASK_SEND    = 1  // need to send IKCP_CMD_WASK
	IKCP_ASK_TELL    = 2  // need to send IKCP_CMD_WINS
	IKCP_WND_SND     = 32
	IKCP_WND_RCV     = 32
	IKCP_MTU_DEF     = 1400
	IKCP_ACK_FAST    = 3
	IKCP_INTERVAL    = 100
	IKCP_OVERHEAD    = 24
	IKCP_DEADLINK    = 10
	IKCP_THRESH_INIT = 2
	IKCP_THRESH_MIN  = 2
	IKCP_PROBE_INIT  = 7000   // 7 secs to probe window size
	IKCP_PROBE_LIMIT = 120000 // up to 120 secs to probe window
)

type Segment struct {
	conv     uint32
	cmd      uint32
	frg      uint32
	ts       uint32
	sn       uint32
	una      uint32
	len      uint32
	resendts uint32
	rto      uint32
	fastack  uint32
	xmit     uint32
	data     []byte
}

func NewSegment(size int32) *Segment {
	seg := new(Segment)
	seg.data = make([]byte, size)
	return seg
}

type KCP struct {
	conv, mtu, mss, state                  uint32
	snd_una, snd_nxt, rcv_nxt              uint32
	ts_recent, ts_lastack, ssthresh        uint32
	rx_rttval, rx_srtt, rx_rto, rx_minrto  int32
	snd_wnd, rcv_wnd, rmt_wnd, cwnd, probe uint32
	current, interval, ts_flush, xmit      uint32
	nodelay, updated                       uint32
	ts_probe, probe_wait                   uint32
	dead_link, incr                        uint32
	/*
		struct IQUEUEHEAD snd_queue;
			struct IQUEUEHEAD rcv_queue;
				struct IQUEUEHEAD snd_buf;
					struct IQUEUEHEAD rcv_buf;
	*/
	rcv_queue []Segment
	rcv_buf   []Segment
	acklist   []uint32
	ackcount  uint32
	ackblock  uint32

	user       interface{}
	buffer     []byte
	fastresend int
	nocwnd     int
	logmask    int
}

func NewKCP(conv uint32) *KCP {
	kcp := new(KCP)
	kcp.conv = conv
	kcp.snd_wnd = IKCP_WND_SND
	kcp.rcv_wnd = IKCP_WND_RCV
	kcp.rmt_wnd = IKCP_WND_RCV
	kcp.mtu = IKCP_MTU_DEF
	kcp.mss = kcp.mtu - IKCP_OVERHEAD
	kcp.buffer = make([]byte, (kcp.mtu+IKCP_OVERHEAD)*3)
	kcp.rx_rto = IKCP_RTO_DEF
	kcp.rx_minrto = IKCP_RTO_MIN
	kcp.interval = IKCP_INTERVAL
	kcp.ts_flush = IKCP_INTERVAL
	kcp.ssthresh = IKCP_THRESH_INIT
	kcp.dead_link = IKCP_DEADLINK
	return kcp
}

func (kcp *KCP) recv(buffer []byte) int {
	if len(kcp.rcv_queue) == 0 {
		return -1
	}

	peeksize := kcp.peeksize()
	if peeksize < 0 {
		return -1
	}

	var fast_recover bool
	if uint32(len(kcp.rcv_queue)) >= kcp.rcv_wnd {
		fast_recover = true
	}

	sz := 0
	for k := range kcp.rcv_queue {
		seg := &kcp.rcv_queue[k]
		if len(buffer) > 0 {
			copy(buffer, seg.data)
			buffer = buffer[len(seg.data):]
		}
		sz += len(seg.data)
		if seg.frg == 0 {
			kcp.rcv_queue = kcp.rcv_queue[k:]
			break
		}
	}
	// move available data from rcv_buf -> rcv_queue
	for k := range kcp.rcv_buf {
		seg := &kcp.rcv_buf[k]
		if seg.sn == kcp.rcv_nxt && uint32(len(kcp.rcv_queue)) < kcp.rcv_wnd {
			kcp.rcv_queue = append(kcp.rcv_queue, *seg)
		} else {
			kcp.rcv_buf = kcp.rcv_buf[k:]
			break
		}
	}

	// fast recover
	if uint32(len(kcp.rcv_queue)) < kcp.rcv_wnd && fast_recover {
		// ready to send back IKCP_CMD_WINS in ikcp_flush
		// tell remote my window size
		kcp.probe |= IKCP_ASK_TELL
	}
	return sz
}

// peek data size
func (kcp *KCP) peeksize() int {
	if len(kcp.rcv_queue) == 0 {
		return -1
	}

	seg := &kcp.rcv_queue[0]
	if seg.frg == 0 {
		return len(seg.data)
	}

	if uint32(len(kcp.rcv_queue)) < seg.frg+1 {
		return -1
	}

	var sz int
	for k := range kcp.rcv_queue {
		seg := &kcp.rcv_queue[k]
		sz += len(seg.data)
		if seg.frg == 0 {
			break
		}
	}
	return sz
}
