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
	for i := range 100 {
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
					return
				}
			}
			continue
		}

		if sent%dataSize == 0 {
			t.Log("no parity:", len(ps))
			continue
		}
	}
}

func TestFECDecodeLoss(t *testing.T) {
	// This function lose 3 random packet from 10 datashards and 3 parity shards.
	// so each group of 13 packets should be able to recover from the loss.
	const dataShards = 10
	const parityShards = 3
	const groupSize = dataShards + parityShards
	const payLoad = 1400
	decoder := newFECDecoder(dataShards, parityShards)
	t.Logf("dataSize:%v, paritySize:%v", dataShards, parityShards)
	sent := 0
	totalRecovered := 0
	totalParityLost := 0

	for group := range 100 {
		losses := make(map[int]bool)

		lost := 0
		parityLost := 0
		for lost < parityShards {
			pos := rand.Intn(dataShards + parityShards)
			if !losses[pos] {
				losses[pos] = true
				if pos >= dataShards {
					totalParityLost++
					parityLost++
				}
				lost++
			}
		}

		if len(losses) != parityShards {
			t.Fatalf("Expected %v losses, got %v", parityShards, len(losses))
			return
		}

		recovered := 0
		for i := range dataShards + parityShards {
			sent++
			if losses[i] {
				t.Logf("Lost packet %v in group %v", groupSize*group+i, group)
				continue
			}

			pkt := make([]byte, payLoad)
			binary.LittleEndian.PutUint32(pkt, uint32(groupSize*group+i))
			if i%(dataShards+parityShards) >= dataShards {
				binary.LittleEndian.PutUint16(pkt[4:], typeParity)
			} else {
				binary.LittleEndian.PutUint16(pkt[4:], typeData)
			}

			rec := decoder.decode(pkt)
			if len(rec) > 0 {
				totalRecovered += len(rec)
				recovered += len(rec)
				t.Log("Recovered", len(rec), "packets from group", group)
			}
		}

		// the recovered packets should equal to the lost data packets
		if recovered != lost-parityLost {
			t.Fatalf("Expected recovered %v packets, got %v", lost-parityLost, recovered)
		}
	}
	t.Log("Total recovered packets:", totalRecovered)
	t.Log("Total parity lost:", totalParityLost)
}

func BenchmarkFECDecode(b *testing.B) {
	const dataSize = 10
	const paritySize = 3
	const payLoad = 1500
	decoder := newFECDecoder(dataSize, paritySize)
	b.ReportAllocs()
	b.SetBytes(payLoad)
	for i := 0; b.Loop(); i++ {
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
	for b.Loop() {
		data := make([]byte, payLoad)
		encoder.encode(data, 200)
	}
}

func TestFECPAWS(t *testing.T) {
	const dataShards = 10
	const parityShards = 3
	const shardSize = dataShards + parityShards
	const payLoad = 1500

	encoder := newFECEncoder(dataShards, parityShards, 0)
	decoder := newFECDecoder(dataShards, parityShards)

	// Manually set the next sequence number to be near the PAWS boundary
	// We want to test the transition from the last group to the first group
	// paws is a multiple of shardSize.
	// We set next to paws - shardSize, so we are at the start of the last group.
	encoder.next = encoder.paws - uint32(shardSize)

	t.Logf("PAWS: %v, Initial Next: %v", encoder.paws, encoder.next)

	var packets []fecPacket

	// 1. Encode the last group before PAWS
	// This will generate 'dataShards' data packets and 'parityShards' parity packets.
	// Total 'shardSize' packets.
	// Their seqids should be [paws-shardSize, ..., paws-1]
	for i := range dataShards {
		data := make([]byte, payLoad)
		// We can put some recognizable data
		// Note: fecEncoder writes header at 0-6, and size at 6-8. Payload starts at 8.
		binary.LittleEndian.PutUint32(data[8:], uint32(i))

		ps := encoder.encode(data, 200)

		// Copy data packet
		pkt := make([]byte, len(data))
		copy(pkt, data)
		packets = append(packets, fecPacket(pkt))

		// Copy parity packets
		for _, p := range ps {
			pCopy := make([]byte, len(p))
			copy(pCopy, p)
			packets = append(packets, fecPacket(pCopy))
		}
	}

	// Verify seqids of the first group
	for i, pkt := range packets {
		seqid := pkt.seqid()
		expected := encoder.paws - uint32(shardSize) + uint32(i)
		if seqid != expected {
			t.Fatalf("Group 1: expected seqid %v, got %v", expected, seqid)
		}
	}
	t.Log("Group 1 generated successfully")

	// 2. Encode the first group after PAWS
	// Their seqids should be [0, ..., shardSize-1]
	startIdx := len(packets)
	for i := range dataShards {
		data := make([]byte, payLoad)
		binary.LittleEndian.PutUint32(data[8:], uint32(i+100)) // Different data

		ps := encoder.encode(data, 200)

		pkt := make([]byte, len(data))
		copy(pkt, data)
		packets = append(packets, fecPacket(pkt))

		for _, p := range ps {
			pCopy := make([]byte, len(p))
			copy(pCopy, p)
			packets = append(packets, fecPacket(pCopy))
		}
	}

	// Verify seqids of the second group
	for i, pkt := range packets[startIdx:] {
		seqid := pkt.seqid()
		expected := uint32(i)
		if seqid != expected {
			t.Fatalf("Group 2: expected seqid %v, got %v", expected, seqid)
		}
	}
	t.Log("Group 2 generated successfully")

	// 3. Feed to decoder with some loss
	// We will lose the last data packet of Group 1 and the first data packet of Group 2
	// to test recovery across the boundary (though recovery is per-group).

	// We drop index 9 (seqid paws-4) and index 13 (seqid 0).
	// Group 1 indices: 0-12. Data: 0-9. Parity: 10-12.
	// Group 2 indices: 13-25. Data: 13-22. Parity: 23-25.

	dropped := make(map[int]bool)
	dropped[9] = true
	dropped[13] = true

	recoveredCount := 0

	for i, pkt := range packets {
		if dropped[i] {
			t.Logf("Dropping packet index %v, seqid %v", i, pkt.seqid())
			continue
		}

		recovered := decoder.decode(pkt)
		if len(recovered) > 0 {
			t.Logf("Recovered %v packets at step %v (seqid %v)", len(recovered), i, pkt.seqid())
			recoveredCount += len(recovered)
			for _, r := range recovered {
				// Verify recovered data
				// r[0:2] is size. r[2:] is payload.
				val := binary.LittleEndian.Uint32(r[2:])
				if val == 9 {
					t.Log("Recovered packet 9 correctly")
				} else if val == 100 {
					t.Log("Recovered packet 13 (val 100) correctly")
				} else {
					t.Errorf("Recovered unexpected data: %v", val)
				}
			}
		}
	}

	if recoveredCount != 2 {
		t.Fatalf("Expected 2 recovered packets, got %v", recoveredCount)
	}

	t.Log("PAWS wrap test passed")
}

func TestFECRTOAndSkipParity(t *testing.T) {
	const dataShards = 3
	const parityShards = 2
	const rto = 50 // 50ms RTO

	enc := newFECEncoder(dataShards, parityShards, 0)

	// Helper to extract seqid from packet
	getSeq := func(b []byte) uint32 {
		return binary.LittleEndian.Uint32(b)
	}

	// --- Scenario 1: Normal case (Time < RTO) ---
	// Send 3 packets quickly
	t.Log("--- Scenario 1: Normal case (Time < RTO) ---")

	// Packet 0
	p0 := make([]byte, 100)
	ps := enc.encode(p0, rto)
	if len(ps) != 0 {
		t.Fatalf("Expected no parity shards yet")
	}
	if seq := getSeq(p0); seq != 0 {
		t.Fatalf("Expected seq 0, got %d", seq)
	}

	// Packet 1
	p1 := make([]byte, 100)
	ps = enc.encode(p1, rto)
	if len(ps) != 0 {
		t.Fatalf("Expected no parity shards yet")
	}
	if seq := getSeq(p1); seq != 1 {
		t.Fatalf("Expected seq 1, got %d", seq)
	}

	// Packet 2 (Trigger parity generation)
	p2 := make([]byte, 100)
	ps = enc.encode(p2, rto)
	if len(ps) != parityShards {
		t.Fatalf("Expected %d parity shards, got %d", parityShards, len(ps))
	}
	if seq := getSeq(p2); seq != 2 {
		t.Fatalf("Expected seq 2, got %d", seq)
	}

	// Check parity seqids
	if seq := getSeq(ps[0]); seq != 3 {
		t.Fatalf("Expected parity[0] seq 3, got %d", seq)
	}
	if seq := getSeq(ps[1]); seq != 4 {
		t.Fatalf("Expected parity[1] seq 4, got %d", seq)
	}

	// --- Scenario 2: Timeout case (Time > RTO) ---
	// Send 2 packets quickly, then sleep, then send 3rd
	t.Log("--- Scenario 2: Timeout case (Time > RTO) ---")

	// Packet 3 (Next data seq should be 5)
	p3 := make([]byte, 100)
	ps = enc.encode(p3, rto)
	if len(ps) != 0 {
		t.Fatalf("Expected no parity shards yet")
	}
	if seq := getSeq(p3); seq != 5 {
		t.Fatalf("Expected seq 5, got %d", seq)
	}

	// Packet 4
	p4 := make([]byte, 100)
	ps = enc.encode(p4, rto)
	if len(ps) != 0 {
		t.Fatalf("Expected no parity shards yet")
	}
	if seq := getSeq(p4); seq != 6 {
		t.Fatalf("Expected seq 6, got %d", seq)
	}

	// Sleep longer than RTO
	time.Sleep(time.Duration(rto+20) * time.Millisecond)

	// Packet 5 (Trigger parity check -> should skip)
	p5 := make([]byte, 100)
	ps = enc.encode(p5, rto)

	// Expect NO parity shards because of timeout
	if len(ps) != 0 {
		t.Fatalf("Expected 0 parity shards due to timeout, got %d", len(ps))
	}
	if seq := getSeq(p5); seq != 7 {
		t.Fatalf("Expected seq 7, got %d", seq)
	}

	// Even though parity was skipped, the sequence ID should have advanced by parityShards (2)
	// So next data packet should be 7 + 1 (current) + 2 (skipped parity) = 10?
	// Wait, let's trace:
	// p5 gets seq 7.
	// encode() calls skipParity() -> enc.next += parityShards.
	// enc.next was 8 (after p5). 8 + 2 = 10.
	// So next packet should have seq 10.

	// --- Verify Sequence ID Growth after Skip ---
	t.Log("--- Verify Sequence ID Growth after Skip ---")

	p6 := make([]byte, 100)
	ps = enc.encode(p6, rto)
	if seq := getSeq(p6); seq != 10 {
		t.Fatalf("Expected seq 10 after skipped parity, got %d", seq)
	}
}
