package kcp

import (
	"encoding/binary"
	"math/rand"
	"testing"
)

func BenchmarkFECDecode(b *testing.B) {
	const dataSize = 10
	const paritySize = 3
	const payLoad = 1500
	decoder := newFECDecoder(dataSize, paritySize)
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
