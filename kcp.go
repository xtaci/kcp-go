package kcp

import "encoding/binary"

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

type Output func(buf []byte, size int32)

/* encode 8 bits unsigned int */
func ikcp_encode8u(p []byte, c byte) []byte {
	p[0] = c
	return p[1:]
}

/* decode 8 bits unsigned int */
func ikcp_decode8u(p []byte, c *byte) []byte {
	*c = p[0]
	return p[1:]
}

/* encode 16 bits unsigned int (lsb) */
func ikcp_encode16u(p []byte, w uint16) []byte {
	binary.LittleEndian.PutUint16(p, w)
	return p[2:]
}

/* decode 16 bits unsigned int (lsb) */
func ikcp_decode16u(p []byte, w *uint16) []byte {
	*w = binary.LittleEndian.Uint16(p)
	return p[2:]
}

/* encode 32 bits unsigned int (lsb) */
func ikcp_encode32u(p []byte, l uint32) []byte {
	binary.LittleEndian.PutUint32(p, l)
	return p[4:]
}

/* decode 32 bits unsigned int (lsb) */
func ikcp_decode32u(p []byte, l *uint32) []byte {
	*l = binary.LittleEndian.Uint32(p)
	return p[4:]
}

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

// KCP Segment Definition
type Segment struct {
	conv     uint32
	cmd      uint32
	frg      uint32
	wnd      uint32
	ts       uint32
	sn       uint32
	una      uint32
	resendts uint32
	rto      uint32
	fastack  uint32
	xmit     uint32
	data     []byte
}

// encode a segment into buffer
func (seg *Segment) encode(ptr []byte) []byte {
	ptr = ikcp_encode32u(ptr, seg.conv)
	ptr = ikcp_encode8u(ptr, uint8(seg.cmd))
	ptr = ikcp_encode8u(ptr, uint8(seg.frg))
	ptr = ikcp_encode16u(ptr, uint16(seg.wnd))
	ptr = ikcp_encode32u(ptr, seg.ts)
	ptr = ikcp_encode32u(ptr, seg.sn)
	ptr = ikcp_encode32u(ptr, seg.una)
	ptr = ikcp_encode32u(ptr, uint32(len(seg.data)))
	return ptr
}

func NewSegment(size uint32) *Segment {
	seg := new(Segment)
	seg.data = make([]byte, size)
	return seg
}

// KCP Connection Definition
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

	buffer     []byte
	fastresend int32
	nocwnd     int32
	logmask    int32
	output     Output
}

func NewKCP(conv uint32, output Output) *KCP {
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
	kcp.output = output
	return kcp
}

// peek data size
func (kcp *KCP) peeksize() (size int) {
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

	for k := range kcp.rcv_queue {
		seg := &kcp.rcv_queue[k]
		size += len(seg.data)
		if seg.frg == 0 {
			break
		}
	}
	return
}

// user/upper level recv: returns size, returns below zero for EAGAIN
func (kcp *KCP) Recv(buffer []byte) (n int) {
	if len(kcp.rcv_queue) == 0 {
		return -1
	}

	peeksize := kcp.peeksize()
	if peeksize < 0 {
		return -2
	}

	if peeksize > len(buffer) {
		return -3
	}

	var fast_recover bool
	if uint32(len(kcp.rcv_queue)) >= kcp.rcv_wnd {
		fast_recover = true
	}

	// merge fragment
	k := 0
	for k = range kcp.rcv_queue {
		seg := &kcp.rcv_queue[k]
		copy(buffer, seg.data)
		buffer = buffer[len(seg.data):]
		n += len(seg.data)
		if seg.frg == 0 {
			break
		}
	}
	kcp.rcv_queue = kcp.rcv_queue[k+1:]

	// move available data from rcv_buf -> rcv_queue
	k = 0
	for k = range kcp.rcv_buf {
		seg := &kcp.rcv_buf[k]
		if seg.sn == kcp.rcv_nxt && uint32(len(kcp.rcv_queue)) < kcp.rcv_wnd {
			kcp.rcv_queue = append(kcp.rcv_queue, *seg)
		} else {
			break
		}
	}
	kcp.rcv_buf = kcp.rcv_buf[k:]

	// fast recover
	if uint32(len(kcp.rcv_queue)) < kcp.rcv_wnd && fast_recover {
		// ready to send back IKCP_CMD_WINS in ikcp_flush
		// tell remote my window size
		kcp.probe |= IKCP_ASK_TELL
	}
	return
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
		sz := kcp.mss
		if len(buffer) <= int(kcp.mss) {
			sz = uint32(len(buffer))
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

func (kcp *KCP) Input(data []byte) int {
	una := kcp.snd_una
	size := len(data)
	if size < 24 {
		return 0
	}

	for {
		var ts, sn, len, una, conv uint32
		var wnd uint16
		var cmd, frg uint8

		if size < int(IKCP_OVERHEAD) {
			break
		}

		data = ikcp_decode32u(data, &conv)
		if conv != kcp.conv {
			return -1
		}

		data = ikcp_decode8u(data, &cmd)
		data = ikcp_decode8u(data, &frg)
		data = ikcp_decode16u(data, &wnd)
		data = ikcp_decode32u(data, &ts)
		data = ikcp_decode32u(data, &sn)
		data = ikcp_decode32u(data, &una)
		data = ikcp_decode32u(data, &len)

		size -= int(IKCP_OVERHEAD)

		if uint32(size) < uint32(len) {
			return -2
		}

		if cmd != uint8(IKCP_CMD_PUSH) && cmd != uint8(IKCP_CMD_ACK) &&
			cmd != uint8(IKCP_CMD_WASK) && cmd != uint8(IKCP_CMD_WINS) {
			return -3
		}

		kcp.rmt_wnd = uint32(wnd)
		kcp.parse_una(una)
		kcp.shrink_buf()

		if cmd == uint8(IKCP_CMD_ACK) {
			if _itimediff(kcp.current, ts) >= 0 {
				kcp.update_ack(_itimediff(kcp.current, ts))
			}
			kcp.parse_ack(sn)
			kcp.shrink_buf()
		} else if cmd == uint8(IKCP_CMD_PUSH) {
			if _itimediff(sn, kcp.rcv_nxt+kcp.rcv_wnd) < 0 {
				kcp.ack_push(sn, ts)
				if _itimediff(sn, kcp.rcv_nxt) >= 0 {
					seg := NewSegment(len)
					seg.conv = conv
					seg.cmd = uint32(cmd)
					seg.frg = uint32(frg)
					seg.wnd = uint32(wnd)
					seg.ts = ts
					seg.sn = sn
					seg.una = una
					copy(seg.data, data[:len])
					kcp.parse_data(seg)
				}
			}
		} else if cmd == uint8(IKCP_CMD_WASK) {
			// ready to send back IKCP_CMD_WINS in Ikcp_flush
			// tell remote my window size
			kcp.probe |= IKCP_ASK_TELL
		} else if cmd == uint8(IKCP_CMD_WINS) {
			// do nothing
		} else {
			return -3
		}

		data = data[len:]
		size -= int(len)
	}

	if _itimediff(kcp.snd_una, una) > 0 {
		if kcp.cwnd < kcp.rmt_wnd {
			mss := kcp.mss
			if kcp.cwnd < kcp.ssthresh {
				kcp.cwnd++
				kcp.incr += mss
			} else {
				if kcp.incr < mss {
					kcp.incr = mss
				}
				kcp.incr += (mss*mss)/kcp.incr + (mss / 16)
				if (kcp.cwnd+1)*mss <= kcp.incr {
					kcp.cwnd++
				}
			}
			if kcp.cwnd > kcp.rmt_wnd {
				kcp.cwnd = kcp.rmt_wnd
				kcp.incr = kcp.rmt_wnd * mss
			}
		}
	}

	return 0
}

func (kcp *KCP) wnd_unused() int32 {
	if uint32(len(kcp.rcv_queue)) < kcp.rcv_wnd {
		return int32(kcp.rcv_wnd) - int32(len(kcp.rcv_queue))
	}
	return 0
}

func (kcp *KCP) flush() {
	current := kcp.current
	buffer := kcp.buffer
	ptr := buffer
	var count, size, i int32
	var resent, cwnd uint32
	var rtomin uint32
	change := 0
	lost := 0

	if kcp.updated == 0 {
		return
	}
	var seg Segment
	seg.conv = kcp.conv
	seg.cmd = IKCP_CMD_ACK
	seg.wnd = uint32(kcp.wnd_unused())
	seg.una = kcp.rcv_nxt

	// flush acknowledges
	size = 0
	count = int32(kcp.ackcount)
	for i = 0; i < count; i++ {
		//size = int32(ptr - buffer)
		if size > int32(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
			size = 0
		}
		kcp.ack_get(i, &seg.sn, &seg.ts)
		ptr = seg.encode(ptr)
		size += 24
	}

	kcp.ackcount = 0

	// probe window size (if remote window size equals zero)
	if kcp.rmt_wnd == 0 {
		if kcp.probe_wait == 0 {
			kcp.probe_wait = IKCP_PROBE_INIT
			kcp.ts_probe = kcp.current + kcp.probe_wait
		} else {
			if _itimediff(kcp.current, kcp.ts_probe) >= 0 {
				if kcp.probe_wait < IKCP_PROBE_INIT {
					kcp.probe_wait = IKCP_PROBE_INIT
				}
				kcp.probe_wait += kcp.probe_wait / 2
				if kcp.probe_wait > IKCP_PROBE_LIMIT {
					kcp.probe_wait = IKCP_PROBE_LIMIT
				}
				kcp.ts_probe = kcp.current + kcp.probe_wait
				kcp.probe |= IKCP_ASK_SEND
			}
		}
	} else {
		kcp.ts_probe = 0
		kcp.probe_wait = 0
	}

	// flush window probing commands
	if (kcp.probe & IKCP_ASK_SEND) != 0 {
		seg.cmd = IKCP_CMD_WASK
		if size > int32(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
			size = 0
		}
		ptr = seg.encode(ptr)
		size += 24
	}

	// flush window probing commands
	if (kcp.probe & IKCP_ASK_TELL) != 0 {
		seg.cmd = IKCP_CMD_WINS
		if size > int32(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
			size = 0
		}
		ptr = seg.encode(ptr)
		size += 24
	}

	kcp.probe = 0

	// calculate window size
	cwnd = _imin_(kcp.snd_wnd, kcp.rmt_wnd)
	if kcp.nocwnd == 0 {
		cwnd = _imin_(kcp.cwnd, cwnd)
	}

	t := 0

	for k := range kcp.snd_queue {
		t++
		if _itimediff(kcp.snd_nxt, kcp.snd_una+cwnd) >= 0 {
			kcp.snd_queue = kcp.snd_queue[:k]
			break
		}
		newseg := kcp.snd_queue[k]
		kcp.snd_buf = append(kcp.snd_buf, newseg)
		newseg.conv = kcp.conv
		newseg.cmd = IKCP_CMD_PUSH
		newseg.wnd = seg.wnd
		newseg.ts = current
		newseg.sn = kcp.snd_nxt
		kcp.snd_nxt++
		newseg.una = kcp.rcv_nxt
		newseg.resendts = current
		newseg.rto = kcp.rx_rto
		newseg.fastack = 0
		newseg.xmit = 0
	}

	// calculate resent
	resent = uint32(kcp.fastresend)
	if kcp.fastresend <= 0 {
		resent = 0xffffffff
	}
	rtomin = (kcp.rx_rto >> 3)
	if kcp.nodelay != 0 {
		rtomin = 0
	}

	a := 0
	// flush data segments
	for k := range kcp.snd_buf {
		////println("debug loop", a, kcp.snd_buf.Len())
		a++
		segment := &kcp.snd_buf[k]
		needsend := 0
		if segment.xmit == 0 {
			needsend = 1
			segment.xmit++
			segment.rto = kcp.rx_rto
			segment.resendts = current + segment.rto + rtomin
		} else if _itimediff(current, segment.resendts) >= 0 {
			needsend = 1
			segment.xmit++
			kcp.xmit++
			if kcp.nodelay == 0 {
				segment.rto += kcp.rx_rto
			} else {
				segment.rto += kcp.rx_rto / 2
			}
			segment.resendts = current + segment.rto
			lost = 1
		} else if segment.fastack >= resent {
			needsend = 1
			segment.xmit++
			segment.fastack = 0
			segment.resendts = current + segment.rto
			change++
		}
		if needsend != 0 {
			var need int32
			segment.ts = current
			segment.wnd = seg.wnd
			segment.una = kcp.rcv_nxt

			need = int32(IKCP_OVERHEAD + len(segment.data))

			if size+need >= int32(kcp.mtu) {
				kcp.output(buffer, size)
				ptr = buffer
				size = 0
			}

			ptr = segment.encode(ptr)
			size += 24

			if len(segment.data) > 0 {
				copy(ptr, segment.data)
				ptr = ptr[len(segment.data):]
				size += int32(len(segment.data))
			}

			if segment.xmit >= kcp.dead_link {
				kcp.state = 0
			}
		}
	}

	// flash remain segments
	if size > 0 {
		kcp.output(buffer, size)
	}

	// update ssthresh
	if change != 0 {
		inflight := kcp.snd_nxt - kcp.snd_una
		kcp.ssthresh = inflight / 2
		if kcp.ssthresh < IKCP_THRESH_MIN {
			kcp.ssthresh = IKCP_THRESH_MIN
		}
		kcp.cwnd = kcp.ssthresh + resent
		kcp.incr = kcp.cwnd * kcp.mss
	}

	if lost != 0 {
		kcp.ssthresh = cwnd / 2
		if kcp.ssthresh < IKCP_THRESH_MIN {
			kcp.ssthresh = IKCP_THRESH_MIN
		}
		kcp.cwnd = 1
		kcp.incr = kcp.mss
	}

	if kcp.cwnd < 1 {
		kcp.cwnd = 1
		kcp.incr = kcp.mss
	}
}

func (kcp *KCP) Update(current uint32) {
	var slap int32

	kcp.current = current

	if kcp.updated == 0 {
		kcp.updated = 1
		kcp.ts_flush = kcp.current
	}

	slap = _itimediff(kcp.current, kcp.ts_flush)

	if slap >= 10000 || slap < -10000 {
		kcp.ts_flush = kcp.current
		slap = 0
	}

	if slap >= 0 {
		kcp.ts_flush += kcp.interval
		if _itimediff(kcp.current, kcp.ts_flush) >= 0 {
			kcp.ts_flush = kcp.current + kcp.interval
		}
		kcp.flush()
	}
}

func (kcp *KCP) check(current uint32) uint32 {
	ts_flush := kcp.ts_flush
	tm_flush := 0x7fffffff
	tm_packet := 0x7fffffff
	minimal := 0
	if kcp.updated == 0 {
		return current
	}

	if _itimediff(current, ts_flush) >= 10000 ||
		_itimediff(current, ts_flush) < -10000 {
		ts_flush = current
	}

	if _itimediff(current, ts_flush) >= 0 {
		return current
	}

	tm_flush = int(_itimediff(ts_flush, current))

	for k := range kcp.snd_buf {
		seg := &kcp.snd_buf[k]
		diff := _itimediff(seg.resendts, current)
		if diff <= 0 {
			return current
		}
		if diff < int32(tm_packet) {
			tm_packet = int(diff)
		}
	}

	minimal = int(tm_packet)
	if tm_packet >= tm_flush {
		minimal = int(tm_flush)
	}
	if uint32(minimal) >= kcp.interval {
		minimal = int(kcp.interval)
	}

	return current + uint32(minimal)
}

// 纯算法协议并不负责探测 MTU，默认 mtu是1400字节，可以使用ikcp_setmtu来设置该值。
// 该值将会影响数据包归并及分片时候的最大传输单元。
func (kcp *KCP) SetMtu(mtu int32) int32 {
	if mtu < 50 || mtu < int32(IKCP_OVERHEAD) {
		return -1
	}
	buffer := make([]byte, (uint32(mtu)+IKCP_OVERHEAD)*3)
	if buffer == nil {
		return -2
	}
	kcp.mtu = uint32(mtu)
	kcp.mss = kcp.mtu - IKCP_OVERHEAD
	kcp.buffer = buffer
	return 0
}

func (kcp *KCP) Interval(interval int32) int32 {
	if interval > 5000 {
		interval = 5000
	} else if interval < 10 {
		interval = 10
	}
	kcp.interval = uint32(interval)
	return 0
}

// nodelay ：是否启用 nodelay模式，0不启用；1启用
// interval ：协议内部工作的 interval，单位毫秒，比如 10ms或者 20ms
// resend ：快速重传模式，默认0关闭，可以设置2（2次ACK跨越将会直接重传）
// nc ：是否关闭流控，默认是0代表不关闭，1代表关闭
// 普通模式： ikcp_nodelay(kcp, 0, 40, 0, 0)
// 极速模式： ikcp_nodelay(kcp, 1, 10, 2, 1);
// 不管是 TCP还是 KCP计算 RTO时都有最小 RTO的限制，即便计算出来RTO为40ms，由于默认的 RTO是100ms，协议只有在100ms后才能检测到丢包，快速模式下为30ms，可以手动更改该值：
func (kcp *KCP) NoDelay(nodelay, interval, resend, nc int32) int32 {
	if nodelay >= 0 {
		kcp.nodelay = uint32(nodelay)
		if nodelay != 0 {
			kcp.rx_minrto = IKCP_RTO_NDL
		} else {
			kcp.rx_minrto = IKCP_RTO_MIN
		}
	}
	if interval >= 0 {
		if interval > 5000 {
			interval = 5000
		} else if interval < 10 {
			interval = 10
		}
		kcp.interval = uint32(interval)
	}
	if resend >= 0 {
		kcp.fastresend = resend
	}
	if nc >= 0 {
		kcp.nocwnd = nc
	}
	return 0
}

// 该调用将会设置协议的最大发送窗口和最大接收窗口大小，默认为32.
// 这个可以理解为 TCP的 SND_BUF 和 RCV_BUF，只不过单位不一样 SND/RCV_BUF 单位是字节，这个单位是包。
func (kcp *KCP) WndSize(sndwnd, rcvwnd int32) int32 {
	if kcp != nil {
		if sndwnd > 0 {
			kcp.snd_wnd = uint32(sndwnd)
		}
		if rcvwnd > 0 {
			kcp.rcv_wnd = uint32(rcvwnd)
		}
	}
	return 0
}

func (kcp *KCP) WaitSnd() int32 {
	return int32(len(kcp.snd_buf) + len(kcp.snd_queue))
}
