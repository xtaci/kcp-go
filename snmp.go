package kcp

import (
	"fmt"
	"sync/atomic"
)

const (
	STAT_XMIT_MAX = 10 //
)

// Snmp defines network statistics indicator
type Snmp struct {
	BytesSent        uint64   // bytes sent from upper level
	BytesReceived    uint64   // bytes received to upper level
	MaxConn          uint64   // max number of connections ever reached
	ActiveOpens      uint64   // accumulated active open connections
	PassiveOpens     uint64   // accumulated passive open connections
	CurrEstab        uint64   // current number of established connections
	DialTimeout      uint64   // dial timeout count
	InErrs           uint64   // UDP read errors reported from net.PacketConn
	InCsumErrors     uint64   // checksum errors from CRC32
	KCPInErrors      uint64   // packet iput errors reported from KCP
	InPkts           uint64   // incoming packets count
	OutPkts          uint64   // outgoing packets count
	InSegs           uint64   // incoming KCP segments
	OutSegs          uint64   // outgoing KCP segments
	InBytes          uint64   // UDP bytes received
	OutBytes         uint64   // UDP bytes sent
	RetransSegs      uint64   // accmulated retransmited segments
	FastRetransSegs  uint64   // accmulated fast retransmitted segments
	EarlyRetransSegs uint64   // accmulated early retransmitted segments
	LostSegs         uint64   // number of segs infered as lost
	RepeatSegs       uint64   // number of segs duplicated
	Parallels        uint64   // parallel count
	AckCost          uint64   // ack cost time total
	AckCount         uint64   // ack count
	XmitInterval     []uint64 // xmit interval
	XmitCount        []uint64 // xmit count
}

func newSnmp() *Snmp {
	snmp := new(Snmp)
	snmp.XmitInterval = make([]uint64, STAT_XMIT_MAX)
	snmp.XmitCount = make([]uint64, STAT_XMIT_MAX)
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
		"AckCost",
		"AckCount",
	}
	headers = append(headers, sliceHeaders("XmitInterval_", STAT_XMIT_MAX)...)
	headers = append(headers, sliceHeaders("XmitCount_", STAT_XMIT_MAX)...)
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
		fmt.Sprint(snmp.AckCost),
		fmt.Sprint(snmp.AckCount),
	}
	vs = append(vs, sliceValues(snmp.XmitInterval)...)
	vs = append(vs, sliceValues(snmp.XmitCount)...)
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
	d.AckCost = atomic.LoadUint64(&s.AckCost)
	d.AckCount = atomic.LoadUint64(&s.AckCount)
	sliceCopy(&d.XmitInterval, s.XmitInterval)
	sliceCopy(&d.XmitCount, s.XmitCount)
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
	atomic.StoreUint64(&s.AckCost, 0)
	atomic.StoreUint64(&s.AckCount, 0)
	sliceReset(s.XmitInterval)
	sliceReset(s.XmitCount)
}

// DefaultSnmp is the global KCP connection statistics collector
var DefaultSnmp *Snmp

func init() {
	DefaultSnmp = newSnmp()
}

func sliceHeaders(header string, count int) []string {
	headers := make([]string, 0, count)
	for i := 0; i < count; i++ {
		headers = append(headers, header+fmt.Sprint(i+1))
	}
	return headers
}

func sliceValues(vi []uint64) []string {
	vs := make([]string, 0, len(vi))
	for i := 0; i < len(vi); i++ {
		vs = append(vs, fmt.Sprint(vi[i]))
	}
	return vs
}

func sliceCopy(tvi *[]uint64, vi []uint64) {
	for i := 0; i < len(vi); i++ {
		(*tvi)[i] = atomic.LoadUint64(&vi[i])
	}
}

func sliceReset(vi []uint64) {
	for i := 0; i < len(vi); i++ {
		atomic.StoreUint64(&vi[i], 0)
	}
}

func statXmitInterval(xmit uint32, interval int32) {
	if 0 < xmit && xmit <= STAT_XMIT_MAX {
		atomic.AddUint64(&DefaultSnmp.XmitInterval[xmit-1], uint64(interval))
		atomic.AddUint64(&DefaultSnmp.XmitCount[xmit-1], 1)
	}
}

func statAck(cost int32) {
	atomic.AddUint64(&DefaultSnmp.AckCost, uint64(cost))
	atomic.AddUint64(&DefaultSnmp.AckCount, 1)
}
