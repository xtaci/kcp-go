// KCP - A Fast and Reliable ARQ Protocol
package kcp

import (
	"encoding/binary"
	"sync"
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

// In general, Output is a closure which captures conn and calls conn.Write
type Output func(buf []byte, size int)

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
	return (int32)(later - earlier)
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

func NewSegment() *Segment {
	return new(Segment)
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

	acklist []uint32

	buffer     []byte
	fastresend int32
	nocwnd     int32
	logmask    int32
	output     Output

	segmentPool sync.Pool
}

// create a new kcp control object, 'conv' must equal in two endpoint
// from the same connection.
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

func (kcp *KCP) acquireSegment(size int) *Segment {
	v := kcp.segmentPool.Get()
	if v == nil {
		seg := NewSegment()
		seg.data = make([]byte, size)
		return seg
	}
	seg := v.(*Segment)
	seg.conv = 0
	seg.cmd = 0
	seg.frg = 0
	seg.wnd = 0
	seg.ts = 0
	seg.sn = 0
	seg.una = 0
	seg.resendts = 0
	seg.rto = 0
	seg.fastack = 0
	seg.xmit = 0
	if cap(seg.data) < size {
		seg.data = make([]byte, size)
	}
	seg.data = seg.data[:size]
	return seg
}

func (kcp *KCP) releaseSegment(seg *Segment) {
	kcp.segmentPool.Put(seg)
}

// check the size of next message in the recv queue
func (kcp *KCP) PeekSize() (length int) {
	if len(kcp.rcv_queue) == 0 {
		return -1
	}

	seg := &kcp.rcv_queue[0]
	if seg.frg == 0 {
		return len(seg.data)
	}

	if len(kcp.rcv_queue) < int(seg.frg+1) {
		return -1
	}

	for k := range kcp.rcv_queue {
		seg := &kcp.rcv_queue[k]
		length += len(seg.data)
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

	peeksize := kcp.PeekSize()
	if peeksize < 0 {
		return -2
	}

	if peeksize > len(buffer) {
		return -3
	}

	var fast_recover bool
	if len(kcp.rcv_queue) >= int(kcp.rcv_wnd) {
		fast_recover = true
	}

	// merge fragment
	count := 0
	for k := range kcp.rcv_queue {
		seg := &kcp.rcv_queue[k]
		copy(buffer, seg.data)
		buffer = buffer[len(seg.data):]
		n += len(seg.data)
		count++
		kcp.releaseSegment(seg)
		if seg.frg == 0 {
			break
		}
	}
	kcp.rcv_queue = kcp.rcv_queue[count:]

	// move available data from rcv_buf -> rcv_queue
	count = 0
	for k := range kcp.rcv_buf {
		seg := &kcp.rcv_buf[k]
		if seg.sn == kcp.rcv_nxt && len(kcp.rcv_queue) < int(kcp.rcv_wnd) {
			kcp.rcv_queue = append(kcp.rcv_queue, *seg)
			kcp.rcv_nxt++
			count++
		} else {
			break
		}
	}
	kcp.rcv_buf = kcp.rcv_buf[count:]

	// fast recover
	if len(kcp.rcv_queue) < int(kcp.rcv_wnd) && fast_recover {
		// ready to send back IKCP_CMD_WINS in ikcp_flush
		// tell remote my window size
		kcp.probe |= IKCP_ASK_TELL
	}
	return
}

// user/upper level send, returns below zero for error
func (kcp *KCP) Send(buffer []byte) int {
	var count int
	if len(buffer) == 0 {
		return -1
	}

	if len(buffer) < int(kcp.mss) {
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
		var size int
		if len(buffer) > int(kcp.mss) {
			size = int(kcp.mss)
		} else {
			size = len(buffer)
		}
		seg := kcp.acquireSegment(size)
		copy(seg.data, buffer[:size])
		seg.frg = uint32(count - i - 1)
		kcp.snd_queue = append(kcp.snd_queue, *seg)
		buffer = buffer[size:]
	}
	return 0
}

func (kcp *KCP) update_ack(rtt int32) {
	var rto uint32 = 0
	if kcp.rx_srtt == 0 {
		kcp.rx_srtt = uint32(rtt)
		kcp.rx_rttval = uint32(rtt) / 2
	} else {
		delta := rtt - int32(kcp.rx_srtt)
		if delta < 0 {
			delta = -delta
		}
		kcp.rx_rttval = (3*kcp.rx_rttval + uint32(delta)) / 4
		kcp.rx_srtt = (7*kcp.rx_srtt + uint32(rtt)) / 8
		if kcp.rx_srtt < 1 {
			kcp.rx_srtt = 1
		}
	}
	rto = kcp.rx_srtt + _imax_(1, 4*kcp.rx_rttval)
	kcp.rx_rto = _ibound_(kcp.rx_minrto, rto, IKCP_RTO_MAX)
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
	count := 0
	for k := range kcp.snd_buf {
		seg := &kcp.snd_buf[k]
		if _itimediff(una, seg.sn) > 0 {
			count++
			kcp.releaseSegment(seg)
		} else {
			break
		}
	}
	kcp.snd_buf = kcp.snd_buf[count:]
}

// ack append
func (kcp *KCP) ack_push(sn, ts uint32) {
	kcp.acklist = append(kcp.acklist, sn, ts)
}

func (kcp *KCP) ack_get(p int) (sn, ts uint32) {
	return kcp.acklist[p*2+0], kcp.acklist[p*2+1]
}

func (kcp *KCP) parse_data(newseg *Segment) {
	sn := newseg.sn
	if _itimediff(sn, kcp.rcv_nxt+kcp.rcv_wnd) >= 0 ||
		_itimediff(sn, kcp.rcv_nxt) < 0 {
		kcp.releaseSegment(newseg)
		return
	}

	n := len(kcp.rcv_buf) - 1
	after_idx := -1
	repeat := false
	for i := n; i >= 0; i-- {
		seg := &kcp.rcv_buf[i]
		if seg.sn == sn {
			repeat = true
			break
		}
		if _itimediff(sn, seg.sn) > 0 {
			after_idx = i
			break
		}
	}

	if !repeat {
		if after_idx == -1 {
			kcp.rcv_buf = append([]Segment{*newseg}, kcp.rcv_buf...)
		} else {
			kcp.rcv_buf = append(kcp.rcv_buf[:after_idx+1], append([]Segment{*newseg}, kcp.rcv_buf[after_idx+1:]...)...)
		}
	} else {
		kcp.releaseSegment(newseg)
	}

	// move available data from rcv_buf -> rcv_queue
	count := 0
	for k := range kcp.rcv_buf {
		seg := &kcp.rcv_buf[k]
		if seg.sn == kcp.rcv_nxt && len(kcp.rcv_queue) < int(kcp.rcv_wnd) {
			kcp.rcv_queue = append(kcp.rcv_queue, kcp.rcv_buf[k])
			kcp.rcv_nxt++
			count++
		} else {
			break
		}
	}
	kcp.rcv_buf = kcp.rcv_buf[count:]
}

// when you received a low level packet (eg. UDP packet), call it
func (kcp *KCP) Input(data []byte) int {
	una := kcp.snd_una
	if len(data) < IKCP_OVERHEAD {
		return 0
	}

	for {
		var ts, sn, length, una, conv uint32
		var wnd uint16
		var cmd, frg uint8

		if len(data) < int(IKCP_OVERHEAD) {
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
		data = ikcp_decode32u(data, &length)
		if len(data) < int(length) {
			return -2
		}

		if cmd != IKCP_CMD_PUSH && cmd != IKCP_CMD_ACK &&
			cmd != IKCP_CMD_WASK && cmd != IKCP_CMD_WINS {
			return -3
		}

		kcp.rmt_wnd = uint32(wnd)
		kcp.parse_una(una)
		kcp.shrink_buf()

		if cmd == IKCP_CMD_ACK {
			if _itimediff(kcp.current, ts) >= 0 {
				kcp.update_ack(_itimediff(kcp.current, ts))
			}
			kcp.parse_ack(sn)
			kcp.shrink_buf()
		} else if cmd == IKCP_CMD_PUSH {
			if _itimediff(sn, kcp.rcv_nxt+kcp.rcv_wnd) < 0 {
				kcp.ack_push(sn, ts)
				if _itimediff(sn, kcp.rcv_nxt) >= 0 {
					seg := kcp.acquireSegment(int(length))
					seg.conv = conv
					seg.cmd = uint32(cmd)
					seg.frg = uint32(frg)
					seg.wnd = uint32(wnd)
					seg.ts = ts
					seg.sn = sn
					seg.una = una
					copy(seg.data, data[:length])
					kcp.parse_data(seg)
				}
			}
		} else if cmd == IKCP_CMD_WASK {
			// ready to send back IKCP_CMD_WINS in Ikcp_flush
			// tell remote my window size
			kcp.probe |= IKCP_ASK_TELL
		} else if cmd == IKCP_CMD_WINS {
			// do nothing
		} else {
			return -3
		}

		data = data[length:]
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
	if len(kcp.rcv_queue) < int(kcp.rcv_wnd) {
		return int32(int(kcp.rcv_wnd) - len(kcp.rcv_queue))
	}
	return 0
}

// flush pending data
func (kcp *KCP) flush() {
	current := kcp.current
	buffer := kcp.buffer
	change := 0
	lost := false

	if kcp.updated == 0 {
		return
	}
	var seg Segment
	seg.conv = kcp.conv
	seg.cmd = IKCP_CMD_ACK
	seg.wnd = uint32(kcp.wnd_unused())
	seg.una = kcp.rcv_nxt

	// flush acknowledges
	count := len(kcp.acklist) / 2
	ptr := buffer
	for i := 0; i < count; i++ {
		size := len(buffer) - len(ptr)
		if size+IKCP_OVERHEAD > int(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
		}
		seg.sn, seg.ts = kcp.ack_get(i)
		ptr = seg.encode(ptr)
	}
	kcp.acklist = nil

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
		size := len(buffer) - len(ptr)
		if size+IKCP_OVERHEAD > int(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
		}
		ptr = seg.encode(ptr)
	}

	// flush window probing commands
	if (kcp.probe & IKCP_ASK_TELL) != 0 {
		seg.cmd = IKCP_CMD_WINS
		size := len(buffer) - len(ptr)
		if size+IKCP_OVERHEAD > int(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
		}
		ptr = seg.encode(ptr)
	}

	kcp.probe = 0

	// calculate window size
	cwnd := _imin_(kcp.snd_wnd, kcp.rmt_wnd)
	if kcp.nocwnd == 0 {
		cwnd = _imin_(kcp.cwnd, cwnd)
	}

	count = 0
	for k := range kcp.snd_queue {
		if _itimediff(kcp.snd_nxt, kcp.snd_una+cwnd) >= 0 {
			break
		}
		newseg := kcp.snd_queue[k]
		newseg.conv = kcp.conv
		newseg.cmd = IKCP_CMD_PUSH
		newseg.wnd = seg.wnd
		newseg.ts = current
		newseg.sn = kcp.snd_nxt
		newseg.una = kcp.rcv_nxt
		newseg.resendts = current
		newseg.rto = kcp.rx_rto
		newseg.fastack = 0
		newseg.xmit = 0
		kcp.snd_buf = append(kcp.snd_buf, newseg)
		kcp.snd_nxt++
		count++
	}
	kcp.snd_queue = kcp.snd_queue[count:]

	// calculate resent
	resent := uint32(kcp.fastresend)
	if kcp.fastresend <= 0 {
		resent = 0xffffffff
	}
	rtomin := (kcp.rx_rto >> 3)
	if kcp.nodelay != 0 {
		rtomin = 0
	}

	// flush data segments
	for k := range kcp.snd_buf {
		segment := &kcp.snd_buf[k]
		needsend := false
		if segment.xmit == 0 {
			needsend = true
			segment.xmit++
			segment.rto = kcp.rx_rto
			segment.resendts = current + segment.rto + rtomin
		} else if _itimediff(current, segment.resendts) >= 0 {
			needsend = true
			segment.xmit++
			kcp.xmit++
			if kcp.nodelay == 0 {
				segment.rto += kcp.rx_rto
			} else {
				segment.rto += kcp.rx_rto / 2
			}
			segment.resendts = current + segment.rto
			lost = true
		} else if segment.fastack >= resent {
			needsend = true
			segment.xmit++
			segment.fastack = 0
			segment.resendts = current + segment.rto
			change++
		}

		if needsend {
			segment.ts = current
			segment.wnd = seg.wnd
			segment.una = kcp.rcv_nxt

			size := len(buffer) - len(ptr)
			need := IKCP_OVERHEAD + len(segment.data)

			if size+need >= int(kcp.mtu) {
				kcp.output(buffer, size)
				ptr = buffer
			}

			ptr = segment.encode(ptr)
			copy(ptr, segment.data)
			ptr = ptr[len(segment.data):]

			if segment.xmit >= kcp.dead_link {
				kcp.state = 0xFFFFFFFF
			}
		}
	}

	// flash remain segments
	size := len(buffer) - len(ptr)
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

	if lost {
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

// update state (call it repeatedly, every 10ms-100ms), or you can ask
// ikcp_check when to call it again (without ikcp_input/_send calling).
// 'current' - current timestamp in millisec.
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

// Determine when should you invoke ikcp_update:
// returns when you should invoke ikcp_update in millisec, if there
// is no ikcp_input/_send calling. you can call ikcp_update in that
// time, instead of call update repeatly.
// Important to reduce unnacessary ikcp_update invoking. use it to
// schedule ikcp_update (eg. implementing an epoll-like mechanism,
// or optimize ikcp_update when handling massive kcp connections)
func (kcp *KCP) Check(current uint32) uint32 {
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

// change MTU size, default is 1400
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

// fastest: ikcp_nodelay(kcp, 1, 20, 2, 1)
// nodelay: 0:disable(default), 1:enable
// interval: internal update timer interval in millisec, default is 100ms
// resend: 0:disable fast resend(default), 1:enable fast resend
// nc: 0:normal congestion control(default), 1:disable congestion control
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

// set maximum window size: sndwnd=32, rcvwnd=32 by default
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

// get how many packet is waiting to be sent
func (kcp *KCP) WaitSnd() int32 {
	return int32(len(kcp.snd_buf) + len(kcp.snd_queue))
}
