package kcp

import (
	"encoding/binary"
	"math/rand"
	"testing"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestFECOther(t *testing.T) {
	if newFEC(128, 0, 1) != nil {
		t.Fail()
	}
	if newFEC(128, 0, 0) != nil {
		t.Fail()
	}
	if newFEC(1, 10, 10) != nil {
		t.Fail()
	}
}

func TestFECNoLost(t *testing.T) {
	fec := newFEC(128, 10, 3)
	for i := 0; i < 100; i += 10 {
		data := makefecgroup(i, 13)
		for k := range data[fec.dataShards] {
			fec.markData(data[k])
		}

		ecc := fec.Encode(data, fecHeaderSize, fecHeaderSize+4)
		for k := range ecc {
			fec.markFEC(ecc[k])
		}
		data = append(data, ecc...)
		for k := range data {
			f := fec.decodeBytes(data[k])
			if recovered := fec.Decode(f); recovered != nil {
				t.Fail()
			}
		}
	}
}

func TestFECLost1(t *testing.T) {
	fec := newFEC(128, 10, 3)
	fec.next = fec.paws - 13
	for i := 0; i < 100; i += 10 {
		data := makefecgroup(i, 13)
		for k := range data[fec.dataShards] {
			fec.markData(data[k])
		}
		ecc := fec.Encode(data, fecHeaderSize, fecHeaderSize+4)
		for k := range ecc {
			fec.markFEC(ecc[k])
		}
		lost := rand.Intn(13)
		for k := range data {
			if k != lost {
				f := fec.decodeBytes(data[k])
				if recovered := fec.Decode(f); recovered != nil {
					if lost > 10 {
						t.Fail()
					}
				}
			}
		}
	}
}

func TestFECLost2(t *testing.T) {
	fec := newFEC(128, 10, 3)
	for i := 0; i < 100; i += 10 {
		data := makefecgroup(i, 13)
		for k := range data[fec.dataShards] {
			fec.markData(data[k])
		}
		ecc := fec.Encode(data, fecHeaderSize, fecHeaderSize+4)
		for k := range ecc {
			fec.markFEC(ecc[k])
		}
		lost1 := rand.Intn(13)
		lost2 := rand.Intn(13)
		for lost2 == lost1 {
			lost2 = rand.Intn(13)
		}
		expect := 0
		if lost1 < 10 {
			expect++
		}
		if lost2 < 10 {
			expect++
		}
		for k := range data {
			if k != lost1 && k != lost2 {
				f := fec.decodeBytes(data[k])
				if recovered := fec.Decode(f); recovered != nil {
					if len(recovered) != expect {
						t.Fail()
					}
				}
			}
		}
	}
}

func makefecgroup(start, size int) (group [][]byte) {
	for i := 0; i < size; i++ {
		data := make([]byte, fecHeaderSize+4)
		binary.LittleEndian.PutUint32(data[fecHeaderSize:], uint32(start+i))
		group = append(group, data)
	}
	return
}
