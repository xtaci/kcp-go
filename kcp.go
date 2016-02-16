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

func _imin_(a, b uint32) uint32 {
	if a <= b {
		return a
	} else {
		return b
	}
}

func _imax_(a, b uint32) uint32 {
	if a >= b {
		return a
	} else {
		return b
	}
}

func _ibound_(lower, middle, upper uint32) uint32 {
	return _imin_(_imax_(lower, middle), upper)
}

func _itimediff(later, earlier uint32) int32 {
	return ((int32)(later - earlier))
}

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

func NewSegment(size int) *Segment {
	seg := new(Segment)
	seg.data = make([]byte, size)
	return seg
}

type KCP struct {
	conv, mtu, mss, state                  uint32
	snd_una, snd_nxt, rcv_nxt              uint32
	ts_recent, ts_lastack, ssthresh        uint32
	rx_rttval, rx_srtt, rx_rto, rx_minrto  uint32
	snd_wnd, rcv_wnd, rmt_wnd, cwnd, probe uint32
	current, interval, ts_flush, xmit      uint32
	nodelay, updated                       uint32
	ts_probe, probe_wait                   uint32
	dead_link, incr                        uint32

	snd_queue []Segment
	rcv_queue []Segment
	snd_buf   []Segment
	rcv_buf   []Segment

	acklist  []uint32
	ackcount uint32
	ackblock uint32

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

func (kcp *KCP) Recv(buffer []byte) int {
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

func (kcp *KCP) Send(buffer []byte) int {
	var count int
	if len(buffer) == 0 {
		return -1
	}

	if uint32(len(buffer)) < kcp.mss {
		count = 1
	} else {
		count = (len(buffer) + int(kcp.mss) - 1) / int(kcp.mss)
	}

	if count > 255 {
		return -2
	}

	if count == 0 {
		count = 1
	}

	for i := 0; i < count; i++ {
		sz := int(kcp.mss)
		if len(buffer) <= int(kcp.mss) {
			sz = len(buffer)
		}
		seg := NewSegment(sz)
		if len(buffer) > 0 {
			copy(seg.data, buffer[:sz])
		}
		seg.frg = uint32(count - i - 1)
		kcp.snd_queue = append(kcp.snd_queue, *seg)
		buffer = buffer[sz:]
	}
	return 0
}

func (kcp *KCP) update_ack(rtt int32) {
	rto := 0
	if kcp.rx_srtt == 0 {
		kcp.rx_srtt = uint32(rtt)
		kcp.rx_rttval = uint32(rtt) / 2
	} else {
		delta := uint32(rtt) - kcp.rx_srtt
		if delta < 0 {
			delta = -delta
		}
		kcp.rx_rttval = (3*kcp.rx_rttval + delta) / 4
		kcp.rx_srtt = (7*kcp.rx_srtt + uint32(rtt)) / 8
		if kcp.rx_srtt < 1 {
			kcp.rx_srtt = 1
		}
	}
	rto = int(kcp.rx_srtt + _imax_(1, 4*kcp.rx_rttval))
	kcp.rx_rto = _ibound_(kcp.rx_minrto, uint32(rto), IKCP_RTO_MAX)
}

func (kcp *KCP) shrink_buf() {
	if len(kcp.snd_buf) > 0 {
		seg := &kcp.snd_buf[0]
		kcp.snd_una = seg.sn
	} else {
		kcp.snd_una = kcp.snd_nxt
	}
}

func (kcp *KCP) parse_ack(sn uint32) {
	if _itimediff(sn, kcp.snd_una) < 0 || _itimediff(sn, kcp.snd_nxt) >= 0 {
		return
	}

	for k := range kcp.snd_buf {
		seg := &kcp.snd_buf[k]
		if sn == seg.sn {
			kcp.snd_buf = append(kcp.snd_buf[:k], kcp.snd_buf[k+1:]...)
			break
		} else {
			seg.fastack++
		}
	}
}

func (kcp *KCP) parse_una(una uint32) {
	for k := range kcp.snd_buf {
		seg := &kcp.snd_buf[k]
		if _itimediff(una, seg.sn) <= 0 {
			kcp.snd_buf = kcp.snd_buf[k:]
			break
		}
	}
}

func (kcp *KCP) ack_push(sn, ts uint32) {
	newsize := kcp.ackcount + 1

	if newsize > kcp.ackblock {
		var acklist []uint32
		var newblock int32

		for newblock = 8; uint32(newblock) < newsize; newblock <<= 1 {
		}
		acklist = make([]uint32, newblock*2)
		if kcp.acklist != nil {
			for x := 0; uint32(x) < kcp.ackcount; x++ {
				acklist[x*2+0] = kcp.acklist[x*2+0]
				acklist[x*2+1] = kcp.acklist[x*2+1]
			}
		}
		kcp.acklist = acklist
		kcp.ackblock = uint32(newblock)
	}

	ptr := kcp.acklist[kcp.ackcount*2:]
	ptr[0] = sn
	ptr[1] = ts
	kcp.ackcount++
}

func (kcp *KCP) ack_get(p int32, sn, ts *uint32) {
	if sn != nil {
		*sn = kcp.acklist[p*2+0]
	}
	if ts != nil {
		*ts = kcp.acklist[p*2+1]
	}
}

func (kcp *KCP) parse_data(newseg *Segment) {
	sn := newseg.sn
	var repeat bool
	if _itimediff(sn, kcp.rcv_nxt+kcp.rcv_wnd) >= 0 ||
		_itimediff(sn, kcp.rcv_nxt) < 0 {
		return
	}

	n := len(kcp.rcv_buf) - 1
	i := -1
	for i = n; i >= 0; i-- {
		seg := &kcp.rcv_buf[i]
		if seg.sn == sn {
			repeat = true
			break
		}
		if _itimediff(sn, seg.sn) > 0 {
			break
		}
	}

	if !repeat {
		if i == -1 {
			kcp.rcv_buf = append(kcp.rcv_buf, *newseg)
		} else {
			kcp.rcv_buf = append(kcp.rcv_buf[:i+1], append([]Segment{*newseg}, kcp.rcv_buf[i+1:]...)...)
		}
	}

	for k := range kcp.rcv_buf {
		seg := &kcp.rcv_buf[k]
		if seg.sn == kcp.rcv_nxt && uint32(len(kcp.rcv_queue)) < kcp.rcv_wnd {
		} else {
			kcp.rcv_queue = append(kcp.rcv_queue, kcp.rcv_buf[:k]...)
			kcp.rcv_buf = kcp.rcv_buf[k:]
			break
		}
	}
}
