package kcp

import (
	"fmt"
	"sync/atomic"
)

var (
	STAT_XMIT_MIN        = 2
	STAT_XMIT_MAX        = 6 //
	STAT_TIME_BUCKET     = []int{40, 120, 360, 720, 1440, 10000}
	STAT_TIME_BUCKET_CNT = len(STAT_TIME_BUCKET)
)

// Snmp defines network statistics indicator
type Snmp struct {
	BytesSent        uint64     // bytes sent from upper level
	BytesReceived    uint64     // bytes received to upper level
	MaxConn          uint64     // max number of connections ever reached
	ActiveOpens      uint64     // accumulated active open connections
	PassiveOpens     uint64     // accumulated passive open connections
	CurrEstab        uint64     // current number of established connections
	DialTimeout      uint64     // dial timeout count
	InErrs           uint64     // UDP read errors reported from net.PacketConn
	InCsumErrors     uint64     // checksum errors from CRC32
	KCPInErrors      uint64     // packet iput errors reported from KCP
	InPkts           uint64     // incoming packets count
	OutPkts          uint64     // outgoing packets count
	InSegs           uint64     // incoming KCP segments
	OutSegs          uint64     // outgoing KCP segments
	InBytes          uint64     // UDP bytes received
	OutBytes         uint64     // UDP bytes sent
	RetransSegs      uint64     // accmulated retransmited segments
	FastRetransSegs  uint64     // accmulated fast retransmitted segments
	EarlyRetransSegs uint64     // accmulated early retransmitted segments
	LostSegs         uint64     // number of segs infered as lost
	RepeatSegs       uint64     // number of segs duplicated
	Parallels        uint64     // parallel count
	AckCost          []uint64   // ack cost time total
	AckCount         []uint64   // ack count
	XmitInterval     [][]uint64 // xmit interval
	XmitCount        [][]uint64 // xmit count
}

func newSnmp() *Snmp {
	snmp := new(Snmp)
	snmp.AckCost = make([]uint64, STAT_TIME_BUCKET_CNT)
	snmp.AckCount = make([]uint64, STAT_TIME_BUCKET_CNT)
	snmp.XmitInterval = make([][]uint64, STAT_XMIT_MAX)
	snmp.XmitCount = make([][]uint64, STAT_XMIT_MAX)
	for i := 0; i < STAT_XMIT_MAX; i++ {
		snmp.XmitInterval[i] = make([]uint64, STAT_TIME_BUCKET_CNT)
		snmp.XmitCount[i] = make([]uint64, STAT_TIME_BUCKET_CNT)
	}
	return snmp
}

// Header returns all field names
func (s *Snmp) Header() []string {
	headers := []string{
		"BytesSent",
		"BytesReceived",
		"MaxConn",
		"ActiveOpens",
		"PassiveOpens",
		"CurrEstab",
		"DialTimeout",
		"InErrs",
		"InCsumErrors",
		"KCPInErrors",
		"InPkts",
		"OutPkts",
		"InSegs",
		"OutSegs",
		"InBytes",
		"OutBytes",
		"RetransSegs",
		"FastRetransSegs",
		"EarlyRetransSegs",
		"LostSegs",
		"RepeatSegs",
		"Parallels",
	}
	headers = append(headers, sliceHeaders1("AckCost", s.AckCost)...)
	headers = append(headers, sliceHeaders1("AckCount", s.AckCount)...)
	headers = append(headers, sliceHeaders2("XmitInterval", s.XmitInterval)...)
	headers = append(headers, sliceHeaders2("XmitCount", s.XmitCount)...)
	return headers
}

// ToSlice returns current snmp info as slice
func (s *Snmp) ToSlice() []string {
	snmp := s.Copy()
	vs := []string{
		fmt.Sprint(snmp.BytesSent),
		fmt.Sprint(snmp.BytesReceived),
		fmt.Sprint(snmp.MaxConn),
		fmt.Sprint(snmp.ActiveOpens),
		fmt.Sprint(snmp.PassiveOpens),
		fmt.Sprint(snmp.CurrEstab),
		fmt.Sprint(snmp.DialTimeout),
		fmt.Sprint(snmp.InErrs),
		fmt.Sprint(snmp.InCsumErrors),
		fmt.Sprint(snmp.KCPInErrors),
		fmt.Sprint(snmp.InPkts),
		fmt.Sprint(snmp.OutPkts),
		fmt.Sprint(snmp.InSegs),
		fmt.Sprint(snmp.OutSegs),
		fmt.Sprint(snmp.InBytes),
		fmt.Sprint(snmp.OutBytes),
		fmt.Sprint(snmp.RetransSegs),
		fmt.Sprint(snmp.FastRetransSegs),
		fmt.Sprint(snmp.EarlyRetransSegs),
		fmt.Sprint(snmp.LostSegs),
		fmt.Sprint(snmp.RepeatSegs),
		fmt.Sprint(snmp.Parallels),
	}
	vs = append(vs, sliceValues1(snmp.AckCost)...)
	vs = append(vs, sliceValues1(snmp.AckCount)...)
	vs = append(vs, sliceValues2(snmp.XmitInterval)...)
	vs = append(vs, sliceValues2(snmp.XmitCount)...)
	return vs
}

// Copy make a copy of current snmp snapshot
func (s *Snmp) Copy() *Snmp {
	d := newSnmp()
	d.BytesSent = atomic.LoadUint64(&s.BytesSent)
	d.BytesReceived = atomic.LoadUint64(&s.BytesReceived)
	d.MaxConn = atomic.LoadUint64(&s.MaxConn)
	d.ActiveOpens = atomic.LoadUint64(&s.ActiveOpens)
	d.PassiveOpens = atomic.LoadUint64(&s.PassiveOpens)
	d.CurrEstab = atomic.LoadUint64(&s.CurrEstab)
	d.DialTimeout = atomic.LoadUint64(&s.DialTimeout)
	d.InErrs = atomic.LoadUint64(&s.InErrs)
	d.InCsumErrors = atomic.LoadUint64(&s.InCsumErrors)
	d.KCPInErrors = atomic.LoadUint64(&s.KCPInErrors)
	d.InPkts = atomic.LoadUint64(&s.InPkts)
	d.OutPkts = atomic.LoadUint64(&s.OutPkts)
	d.InSegs = atomic.LoadUint64(&s.InSegs)
	d.OutSegs = atomic.LoadUint64(&s.OutSegs)
	d.InBytes = atomic.LoadUint64(&s.InBytes)
	d.OutBytes = atomic.LoadUint64(&s.OutBytes)
	d.RetransSegs = atomic.LoadUint64(&s.RetransSegs)
	d.FastRetransSegs = atomic.LoadUint64(&s.FastRetransSegs)
	d.EarlyRetransSegs = atomic.LoadUint64(&s.EarlyRetransSegs)
	d.LostSegs = atomic.LoadUint64(&s.LostSegs)
	d.RepeatSegs = atomic.LoadUint64(&s.RepeatSegs)
	d.Parallels = atomic.LoadUint64(&s.Parallels)
	sliceCopy1(&d.AckCost, s.AckCost)
	sliceCopy1(&d.AckCount, s.AckCount)
	sliceCopy2(&d.XmitInterval, s.XmitInterval)
	sliceCopy2(&d.XmitCount, s.XmitCount)
	return d
}

// Reset values to zero
func (s *Snmp) Reset() {
	atomic.StoreUint64(&s.BytesSent, 0)
	atomic.StoreUint64(&s.BytesReceived, 0)
	atomic.StoreUint64(&s.MaxConn, 0)
	atomic.StoreUint64(&s.ActiveOpens, 0)
	atomic.StoreUint64(&s.PassiveOpens, 0)
	atomic.StoreUint64(&s.CurrEstab, 0)
	atomic.StoreUint64(&s.DialTimeout, 0)
	atomic.StoreUint64(&s.InErrs, 0)
	atomic.StoreUint64(&s.InCsumErrors, 0)
	atomic.StoreUint64(&s.KCPInErrors, 0)
	atomic.StoreUint64(&s.InPkts, 0)
	atomic.StoreUint64(&s.OutPkts, 0)
	atomic.StoreUint64(&s.InSegs, 0)
	atomic.StoreUint64(&s.OutSegs, 0)
	atomic.StoreUint64(&s.InBytes, 0)
	atomic.StoreUint64(&s.OutBytes, 0)
	atomic.StoreUint64(&s.RetransSegs, 0)
	atomic.StoreUint64(&s.FastRetransSegs, 0)
	atomic.StoreUint64(&s.EarlyRetransSegs, 0)
	atomic.StoreUint64(&s.LostSegs, 0)
	atomic.StoreUint64(&s.RepeatSegs, 0)
	atomic.StoreUint64(&s.Parallels, 0)
	sliceReset1(s.AckCost)
	sliceReset1(s.AckCount)
	sliceReset2(s.XmitInterval)
	sliceReset2(s.XmitCount)
}

// DefaultSnmp is the global KCP connection statistics collector
var DefaultSnmp *Snmp

func init() {
	DefaultSnmp = newSnmp()
}

func sliceHeaders2(header string, vss [][]uint64) []string {
	ret := make([]string, 0, len(vss)*len(vss[0]))
	for i := 0; i < len(vss); i++ {
		headers := sliceHeaders1(header+"_"+fmt.Sprint(i+1), vss[i])
		ret = append(ret, headers...)
	}
	return ret
}

func sliceHeaders1(header string, vs []uint64) []string {
	ret := make([]string, 0, len(vs))
	for i := 0; i < len(vs); i++ {
		ret = append(ret, header+"_"+fmt.Sprint(i+1))
	}
	return ret
}

func sliceValues2(vss [][]uint64) []string {
	ret := make([]string, 0, len(vss)*len(vss[0]))
	for i := 0; i < len(vss); i++ {
		vs := sliceValues1(vss[i])
		ret = append(ret, vs...)
	}
	return ret
}

func sliceValues1(vs []uint64) []string {
	ret := make([]string, 0, len(vs))
	for i := 0; i < len(vs); i++ {
		ret = append(ret, fmt.Sprint(vs[i]))
	}
	return ret
}

func sliceCopy2(tvi *[][]uint64, vi [][]uint64) {
	for i := 0; i < len(vi); i++ {
		sliceCopy1(&(*tvi)[i], vi[i])
	}
}

func sliceCopy1(tvi *[]uint64, vi []uint64) {
	for i := 0; i < len(vi); i++ {
		(*tvi)[i] = atomic.LoadUint64(&vi[i])
	}
}

func sliceReset2(vi [][]uint64) {
	for i := 0; i < len(vi); i++ {
		sliceReset1(vi[i])
	}
}

func sliceReset1(vi []uint64) {
	for i := 0; i < len(vi); i++ {
		atomic.StoreUint64(&vi[i], 0)
	}
}

func findBucketIdx(time int) int {
	var idx int = 0
	for ; idx < STAT_TIME_BUCKET_CNT; idx++ {
		if time <= STAT_TIME_BUCKET[idx] {
			return idx
		}
	}
	//normally will not go here
	return idx - 1
}

func statXmitInterval(xmit int, interval int) {
	if xmit < STAT_XMIT_MIN || xmit > STAT_XMIT_MAX {
		return
	}
	idx := findBucketIdx(interval)
	atomic.AddUint64(&DefaultSnmp.XmitInterval[xmit-1][idx], uint64(interval))
	atomic.AddUint64(&DefaultSnmp.XmitCount[xmit-1][idx], 1)
}

func statAck(xmit int, cost int) {
	if xmit < STAT_XMIT_MIN || xmit > STAT_XMIT_MAX {
		return
	}
	idx := findBucketIdx(cost)
	atomic.AddUint64(&DefaultSnmp.AckCost[idx], uint64(cost))
	atomic.AddUint64(&DefaultSnmp.AckCount[idx], 1)
}
