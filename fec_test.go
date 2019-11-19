package kcp

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
)

func BenchmarkFECDecode(b *testing.B) {
	const dataSize = 10
	const paritySize = 3
	const payLoad = 1500
	decoder := newFECDecoder(1024, dataSize, paritySize)
	b.ReportAllocs()
	b.SetBytes(payLoad)
	for i := 0; i < b.N; i++ {
		if rand.Int()%(dataSize+paritySize) == 0 { // random loss
			continue
		}
		pkt := make([]byte, payLoad)
		binary.LittleEndian.PutUint32(pkt, uint32(i))
		if i%(dataSize+paritySize) >= dataSize {
			binary.LittleEndian.PutUint16(pkt[4:], typeParity)
		} else {
			binary.LittleEndian.PutUint16(pkt[4:], typeData)
		}
		decoder.decode(pkt)
	}
}

func BenchmarkFECEncode(b *testing.B) {
	const dataSize = 10
	const paritySize = 3
	const payLoad = 1500

	b.ReportAllocs()
	b.SetBytes(payLoad)
	encoder := newFECEncoder(dataSize, paritySize, 0)
	for i := 0; i < b.N; i++ {
		data := make([]byte, payLoad)
		encoder.encode(data)
	}
}

func TestForceEncodeAndDecode(t *testing.T) {
	const dataSize = 10
	const paritySize = 3
	const payLoad = 1500

	encoder := newFECEncoder(dataSize, paritySize, 0)

	payload2Make := dataSize / 2

	payloadBuffer := make([][]byte, payload2Make)

	// only half of datasize in shard cache
	for i := 0; i < payload2Make; i++ {
		data := make([]byte, payLoad)
		payloadBuffer[i] = data
		binary.LittleEndian.PutUint32(data, uint32(i))
		encoder.encode(data)
	}

	padding2Make := dataSize - payload2Make
	pb, ps := encoder.forceEncode()
	if len(pb) != padding2Make {
		t.Fatalf("generated padding %d not equal required %d", len(pb), padding2Make)
	}
	if len(ps) != paritySize {
		t.Fatalf("generated parityshard %d less not equal to requird %d", len(ps), paritySize)
	}
	for i := range pb {
		if fecPacket(pb[i]).flag() != typePadding {
			t.Fatalf("non-padding type appears in paddings")
		}
	}
	for i := range ps {
		if fecPacket(ps[i]).flag() != typeParity {
			t.Fatalf("non-partishard type appears in partishards")
		}
	}

	decoder := newFECDecoder(1024, dataSize, paritySize)
LostPayloadIterator:
	for lostPayload := 1; lostPayload <= payload2Make; lostPayload++ {
		if (payload2Make-lostPayload)+padding2Make+paritySize < dataSize {
			break
		}
		payload2Recover := payloadBuffer[:lostPayload]
		payloadBufferSlice := payloadBuffer[lostPayload:]
		for i := range payloadBufferSlice {
			decoder.decode(payloadBufferSlice[i])
		}
		// recover all padding by single padding
		if len(pb) > 0 {
			decoder.decode(pb[0])
		}
		for i := range ps {
			recovered := decoder.decode(ps[i])
			if recovered == nil {
				continue
			} else {
			RecoveredCompare:
				for r := range recovered {
					for p := range payload2Recover {
						if bytes.Compare(payload2Recover[r], payload2Recover[p]) == 0 {
							payload2Recover[p] = nil
							continue RecoveredCompare
						}
					}
					t.Fatalf("recovered payload not equal to origin payload")
				}
				continue LostPayloadIterator
			}
		}
		t.Fatalf("can not recover payload")
	}
}
