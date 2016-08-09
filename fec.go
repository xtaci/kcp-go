package kcp

import (
	"encoding/binary"
	"log"
	"sync"

	"github.com/klauspost/reedsolomon"
)

const (
	fecHeaderSize      = 6
	fecHeaderSizePlus2 = fecHeaderSize + 2 // plus 2B data size
	typeData           = 0xf1
	typeFEC            = 0xf2
	fecExpire          = 30000 // 30s
	rxFecLimit         = 2048
)

type (
	// FEC defines forward error correction for packets
	FEC struct {
		rx           []fecPacket // ordered receive queue
		rxlimit      int         // queue size limit
		dataShards   int
		parityShards int
		shardSize    int
		next         uint32 // next seqid
		enc          reedsolomon.Encoder
		shards       [][]byte
		shardsflag   []bool
		paws         uint32 // Protect Against Wrapped Sequence numbers
		lastCheck    uint32
		xmitBuf      sync.Pool
	}

	fecPacket struct {
		seqid uint32
		flag  uint16
		data  []byte
		ts    uint32
	}
)

func NewFEC(dataShards, parityShards int) *FEC {
	if dataShards <= 0 || parityShards <= 0 {
		return nil
	}
	if rxFecLimit < dataShards+parityShards {
		return nil
	}

	fec := new(FEC)
	fec.rxlimit = rxFecLimit
	fec.dataShards = dataShards
	fec.parityShards = parityShards
	fec.shardSize = dataShards + parityShards
	fec.paws = (0xffffffff/uint32(fec.shardSize) - 1) * uint32(fec.shardSize)
	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		log.Println(err)
		return nil
	}
	fec.enc = enc
	fec.shards = make([][]byte, fec.shardSize)
	fec.shardsflag = make([]bool, fec.shardSize)
	fec.xmitBuf.New = func() interface{} {
		return make([]byte, mtuLimit)
	}

	return fec
}

// decode a fec packet
func (fec *FEC) decode(data []byte) fecPacket {
	var pkt fecPacket
	pkt.seqid = binary.LittleEndian.Uint32(data)
	pkt.flag = binary.LittleEndian.Uint16(data[4:])
	pkt.ts = currentMs()
	// allocate memory & copy
	buf := fec.xmitBuf.Get().([]byte)
	copy(buf, data[6:])
	n := len(data[6:])
	xorBytes(buf[n:], buf[n:], buf[n:])
	pkt.data = buf
	return pkt
}

func (fec *FEC) markData(data []byte) {
	binary.LittleEndian.PutUint32(data, fec.next)
	binary.LittleEndian.PutUint16(data[4:], typeData)
	fec.next++
}

func (fec *FEC) markFEC(data []byte) {
	binary.LittleEndian.PutUint32(data, fec.next)
	binary.LittleEndian.PutUint16(data[4:], typeFEC)
	fec.next++
	if fec.next >= fec.paws { // paws would only occurs in markFEC
		fec.next = 0
	}
}

// input a fec packet
func (fec *FEC) input(pkt fecPacket) (recovered [][]byte) {
	// expiration
	now := currentMs()
	if now-fec.lastCheck >= fecExpire {
		var rx []fecPacket
		for k := range fec.rx {
			if now-fec.rx[k].ts < fecExpire {
				rx = append(rx, fec.rx[k])
			} else {
				fec.xmitBuf.Put(fec.rx[k].data)
			}
		}
		fec.rx = rx
		fec.lastCheck = now
	}

	// insertion
	n := len(fec.rx) - 1
	insertIdx := 0
	for i := n; i >= 0; i-- {
		if pkt.seqid == fec.rx[i].seqid { // de-duplicate
			fec.xmitBuf.Put(pkt.data)
			return nil
		} else if pkt.seqid > fec.rx[i].seqid { // insertion
			insertIdx = i + 1
			break
		}
	}

	// insert into ordered rx queue
	if insertIdx == n+1 {
		fec.rx = append(fec.rx, pkt)
	} else {
		fec.rx = append(fec.rx, fecPacket{})
		copy(fec.rx[insertIdx+1:], fec.rx[insertIdx:])
		fec.rx[insertIdx] = pkt
	}

	// shard range for current packet
	shardBegin := pkt.seqid - pkt.seqid%uint32(fec.shardSize)
	shardEnd := shardBegin + uint32(fec.shardSize) - 1

	// max search range in ordered queue for current shard
	searchBegin := insertIdx - int(pkt.seqid%uint32(fec.shardSize))
	if searchBegin < 0 {
		searchBegin = 0
	}
	searchEnd := searchBegin + fec.shardSize - 1
	if searchEnd >= len(fec.rx) {
		searchEnd = len(fec.rx) - 1
	}

	if searchEnd > searchBegin && searchEnd-searchBegin+1 >= fec.dataShards {
		numshard := 0
		numDataShard := 0
		first := -1
		maxlen := 0
		shards := fec.shards
		shardsflag := fec.shardsflag
		for k := range fec.shards {
			shards[k] = nil
			shardsflag[k] = false
		}

		for i := searchBegin; i <= searchEnd; i++ {
			seqid := fec.rx[i].seqid
			if seqid > shardEnd {
				break
			} else if seqid >= shardBegin {
				shards[seqid%uint32(fec.shardSize)] = fec.rx[i].data
				shardsflag[seqid%uint32(fec.shardSize)] = true
				numshard++
				if fec.rx[i].flag == typeData {
					numDataShard++
				}
				if numshard == 1 {
					first = i
				}
				if len(fec.rx[i].data) > maxlen {
					maxlen = len(fec.rx[i].data)
				}
			}
		}

		if numDataShard == fec.dataShards { // no lost
			for i := first; i < first+numshard; i++ { // free
				fec.xmitBuf.Put(fec.rx[i].data)
			}
			copy(fec.rx[first:], fec.rx[first+numshard:])
			for i := 0; i < numshard; i++ { // dereference
				fec.rx[len(fec.rx)-1-i] = fecPacket{}
			}
			fec.rx = fec.rx[:len(fec.rx)-numshard]
		} else if numshard >= fec.dataShards { // recoverable
			for k := range shards {
				if shards[k] != nil {
					shards[k] = shards[k][:maxlen]
				}
			}
			if err := fec.enc.Reconstruct(shards); err == nil {
				for k := range shards[:fec.dataShards] {
					if !shardsflag[k] {
						recovered = append(recovered, shards[k])
					}
				}
			} else {
				log.Println(err)
			}

			for i := first; i < first+numshard; i++ { // free
				fec.xmitBuf.Put(fec.rx[i].data)
			}
			copy(fec.rx[first:], fec.rx[first+numshard:])
			for i := 0; i < numshard; i++ { // dereference
				fec.rx[len(fec.rx)-1-i] = fecPacket{}
			}
			fec.rx = fec.rx[:len(fec.rx)-numshard]
		}
	}

	// keep rxlimit
	if len(fec.rx) > fec.rxlimit {
		fec.xmitBuf.Put(fec.rx[0].data) // free
		fec.rx[0].data = nil
		fec.rx = fec.rx[1:]
	}
	return
}

func (fec *FEC) calcECC(data [][]byte, offset, maxlen int) (ecc [][]byte) {
	if len(data) != fec.shardSize {
		log.Println("mismatch", len(data), fec.shardSize)
		return nil
	}
	shards := make([][]byte, fec.shardSize)
	for k := range shards {
		shards[k] = data[k][offset:maxlen]
	}

	if err := fec.enc.Encode(shards); err != nil {
		log.Println(err)
		return nil
	}
	return data[fec.dataShards:]
}
