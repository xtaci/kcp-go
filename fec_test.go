// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package kcp

import (
	"encoding/binary"
	"math/rand"
	"testing"
	"time"
)

func TestFECEncodeConsecutive(t *testing.T) {
	const dataSize = 10
	const paritySize = 3
	const payLoad = 1500

	encoder := newFECEncoder(dataSize, paritySize, 0)
	t.Logf("dataSize:%v, paritySize:%v", dataSize, paritySize)
	group := 0
	sent := 0
	for i := 0; i < 100; i++ {
		if i%dataSize == 0 {
			group++
		}

		data := make([]byte, payLoad)
		duration := time.Duration(rand.Int()%300) * time.Millisecond
		t.Logf("Sleep: %v, packet %v", duration, sent)
		<-time.After(duration)

		ps := encoder.encode(data, 200)
		sent++

		if len(ps) > 0 {
			t.Log("has parity:", len(ps))
			for idx, p := range ps {
				seqid := binary.LittleEndian.Uint32(p)
				expected := uint32((group-1)*(dataSize+paritySize) + dataSize + idx)
				if seqid != expected {
					t.Fatalf("expected parity shard:%v actual seqid %v", expected, seqid)
				}
			}
		} else if sent%dataSize == 0 {
			t.Log("no parity:", len(ps))
		}
	}
}

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
		encoder.encode(data, 200)
	}
}
