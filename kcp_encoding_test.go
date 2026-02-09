package kcp

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// sink variables to prevent dead-code elimination by the compiler
var (
	sinkHeader segmentHeader
	sinkBytes  []byte
)

// buildTestPacket creates a realistic KCP packet with N segments for benchmarking.
// Each segment has a header + payload, simulating what Input() actually processes.
func buildTestPacket(nsegs int, payloadSize int) []byte {
	pkt := make([]byte, 0, nsegs*(IKCP_OVERHEAD+payloadSize))
	for i := 0; i < nsegs; i++ {
		hdr := make([]byte, IKCP_OVERHEAD+payloadSize)
		binary.LittleEndian.PutUint32(hdr[0:], 0x12345678)
		hdr[4] = IKCP_CMD_PUSH
		hdr[5] = 0
		binary.LittleEndian.PutUint16(hdr[6:], 128)
		binary.LittleEndian.PutUint32(hdr[8:], uint32(1000+i))
		binary.LittleEndian.PutUint32(hdr[12:], uint32(i))
		binary.LittleEndian.PutUint32(hdr[16:], 0)
		binary.LittleEndian.PutUint32(hdr[20:], uint32(payloadSize))
		pkt = append(pkt, hdr...)
	}
	return pkt
}

// -----------------------------------------------------------------------
// Single header encode/decode benchmarks
// -----------------------------------------------------------------------

func BenchmarkEncode(b *testing.B) {
	b.Run("New_encodeSegHeader", func(b *testing.B) {
		buf := make([]byte, IKCP_OVERHEAD)
		h := segmentHeader{
			conv: 0x12345678, cmd: IKCP_CMD_PUSH, frg: 3,
			wnd: 128, ts: 1000, sn: 42, una: 40, length: 1024,
		}
		b.SetBytes(IKCP_OVERHEAD)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			encodeSegHeader(buf, h)
		}
		sinkBytes = buf
	})

	b.Run("Old_binary_PutUintXX", func(b *testing.B) {
		buf := make([]byte, IKCP_OVERHEAD)
		h := segmentHeader{
			conv: 0x12345678, cmd: IKCP_CMD_PUSH, frg: 3,
			wnd: 128, ts: 1000, sn: 42, una: 40, length: 1024,
		}
		b.SetBytes(IKCP_OVERHEAD)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			binary.LittleEndian.PutUint32(buf, h.conv)
			buf[4] = h.cmd
			buf[5] = h.frg
			binary.LittleEndian.PutUint16(buf[6:], h.wnd)
			binary.LittleEndian.PutUint32(buf[8:], h.ts)
			binary.LittleEndian.PutUint32(buf[12:], h.sn)
			binary.LittleEndian.PutUint32(buf[16:], h.una)
			binary.LittleEndian.PutUint32(buf[20:], h.length)
		}
		sinkBytes = buf
	})
}

func BenchmarkDecode(b *testing.B) {
	buf := make([]byte, IKCP_OVERHEAD)
	encodeSegHeader(buf, segmentHeader{
		conv: 0x12345678, cmd: IKCP_CMD_PUSH, frg: 3,
		wnd: 128, ts: 1000, sn: 42, una: 40, length: 1024,
	})

	b.Run("New_decodeSegHeader", func(b *testing.B) {
		b.SetBytes(IKCP_OVERHEAD)
		b.ResetTimer()
		var h segmentHeader
		for i := 0; i < b.N; i++ {
			decodeSegHeader(buf, &h)
		}
		sinkHeader = h
	})

	b.Run("Old_binary_UintXX", func(b *testing.B) {
		b.SetBytes(IKCP_OVERHEAD)
		b.ResetTimer()
		var h segmentHeader
		for i := 0; i < b.N; i++ {
			h.conv = binary.LittleEndian.Uint32(buf)
			h.cmd = buf[4]
			h.frg = buf[5]
			h.wnd = binary.LittleEndian.Uint16(buf[6:])
			h.ts = binary.LittleEndian.Uint32(buf[8:])
			h.sn = binary.LittleEndian.Uint32(buf[12:])
			h.una = binary.LittleEndian.Uint32(buf[16:])
			h.length = binary.LittleEndian.Uint32(buf[20:])
		}
		sinkHeader = h
	})
}

// -----------------------------------------------------------------------
// Multi-segment decode loop: simulates real Input() hot path
// Both versions write to the same local struct variable and store to the
// global sink only once after the loop, ensuring equal side-effects.
// -----------------------------------------------------------------------

func BenchmarkDecodeLoop(b *testing.B) {
	for _, nsegs := range []int{1, 4, 16, 64} {
		pkt := buildTestPacket(nsegs, 200)

		b.Run(fmt.Sprintf("%dsegs/New_decodeSegHeader", nsegs), func(b *testing.B) {
			b.SetBytes(int64(len(pkt)))
			b.ResetTimer()
			var h segmentHeader
			for i := 0; i < b.N; i++ {
				data := pkt
				for len(data) >= IKCP_OVERHEAD {
					decodeSegHeader(data, &h)
					data = data[IKCP_OVERHEAD+h.length:]
				}
			}
			sinkHeader = h
		})

		b.Run(fmt.Sprintf("%dsegs/Old_binary_UintXX", nsegs), func(b *testing.B) {
			b.SetBytes(int64(len(pkt)))
			b.ResetTimer()
			var h segmentHeader
			for i := 0; i < b.N; i++ {
				data := pkt
				for len(data) >= IKCP_OVERHEAD {
					h.conv = binary.LittleEndian.Uint32(data)
					h.cmd = data[4]
					h.frg = data[5]
					h.wnd = binary.LittleEndian.Uint16(data[6:])
					h.ts = binary.LittleEndian.Uint32(data[8:])
					h.sn = binary.LittleEndian.Uint32(data[12:])
					h.una = binary.LittleEndian.Uint32(data[16:])
					h.length = binary.LittleEndian.Uint32(data[20:])
					data = data[IKCP_OVERHEAD+h.length:]
				}
			}
			sinkHeader = h
		})
	}
}

// -----------------------------------------------------------------------
// Multi-segment encode loop: simulates real flush() hot path
// -----------------------------------------------------------------------

func BenchmarkEncodeLoop(b *testing.B) {
	for _, nsegs := range []int{1, 4, 16, 64} {
		buf := make([]byte, nsegs*(IKCP_OVERHEAD+200))

		b.Run(fmt.Sprintf("%dsegs/New_encodeSegHeader", nsegs), func(b *testing.B) {
			b.SetBytes(int64(nsegs * IKCP_OVERHEAD))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ptr := buf
				for j := 0; j < nsegs; j++ {
					encodeSegHeader(ptr, segmentHeader{
						conv: 0x12345678, cmd: IKCP_CMD_PUSH, frg: 0,
						wnd: 128, ts: uint32(1000 + j), sn: uint32(j),
						una: 0, length: 200,
					})
					ptr = ptr[IKCP_OVERHEAD+200:]
				}
			}
			sinkBytes = buf
		})

		b.Run(fmt.Sprintf("%dsegs/Old_binary_PutUintXX", nsegs), func(b *testing.B) {
			b.SetBytes(int64(nsegs * IKCP_OVERHEAD))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ptr := buf
				for j := 0; j < nsegs; j++ {
					binary.LittleEndian.PutUint32(ptr, 0x12345678)
					ptr[4] = IKCP_CMD_PUSH
					ptr[5] = 0
					binary.LittleEndian.PutUint16(ptr[6:], 128)
					binary.LittleEndian.PutUint32(ptr[8:], uint32(1000+j))
					binary.LittleEndian.PutUint32(ptr[12:], uint32(j))
					binary.LittleEndian.PutUint32(ptr[16:], 0)
					binary.LittleEndian.PutUint32(ptr[20:], 200)
					ptr = ptr[IKCP_OVERHEAD+200:]
				}
			}
			sinkBytes = buf
		})
	}
}

// TestSegmentHeaderRoundTrip verifies encode/decode correctness
func TestSegmentHeaderRoundTrip(t *testing.T) {
	original := segmentHeader{
		conv:   0xDEADBEEF,
		cmd:    IKCP_CMD_ACK,
		frg:    7,
		wnd:    256,
		ts:     0xCAFEBABE,
		sn:     12345,
		una:    12340,
		length: 500,
	}

	buf := make([]byte, IKCP_OVERHEAD)
	encodeSegHeader(buf, original)
	var decoded segmentHeader
	decodeSegHeader(buf, &decoded)

	if decoded != original {
		t.Fatalf("round-trip failed:\n  original: %+v\n  decoded:  %+v", original, decoded)
	}

	// Also verify wire format compatibility with binary.LittleEndian
	if binary.LittleEndian.Uint32(buf[0:]) != original.conv {
		t.Fatal("conv wire format mismatch")
	}
	if buf[4] != original.cmd {
		t.Fatal("cmd wire format mismatch")
	}
	if buf[5] != original.frg {
		t.Fatal("frg wire format mismatch")
	}
	if binary.LittleEndian.Uint16(buf[6:]) != original.wnd {
		t.Fatal("wnd wire format mismatch")
	}
	if binary.LittleEndian.Uint32(buf[8:]) != original.ts {
		t.Fatal("ts wire format mismatch")
	}
	if binary.LittleEndian.Uint32(buf[12:]) != original.sn {
		t.Fatal("sn wire format mismatch")
	}
	if binary.LittleEndian.Uint32(buf[16:]) != original.una {
		t.Fatal("una wire format mismatch")
	}
	if binary.LittleEndian.Uint32(buf[20:]) != original.length {
		t.Fatal("length wire format mismatch")
	}
}

// TestSegmentHeaderEncode verifies segment.encode() uses the optimized path correctly
func TestSegmentHeaderEncode(t *testing.T) {
	seg := segment{
		conv: 0x11223344,
		cmd:  IKCP_CMD_PUSH,
		frg:  2,
		wnd:  64,
		ts:   9999,
		sn:   100,
		una:  98,
		data: make([]byte, 200),
	}

	buf := make([]byte, IKCP_OVERHEAD+len(seg.data))
	rest := seg.encode(buf)

	if len(rest) != len(seg.data) {
		t.Fatalf("encode returned wrong remaining length: %d", len(rest))
	}

	var hdr segmentHeader
	decodeSegHeader(buf, &hdr)
	if hdr.conv != seg.conv || hdr.cmd != seg.cmd || hdr.frg != seg.frg ||
		hdr.wnd != seg.wnd || hdr.ts != seg.ts || hdr.sn != seg.sn ||
		hdr.una != seg.una || hdr.length != uint32(len(seg.data)) {
		t.Fatalf("segment.encode() header mismatch:\n  segment: conv=%x cmd=%d frg=%d wnd=%d ts=%d sn=%d una=%d datalen=%d\n  header:  %+v",
			seg.conv, seg.cmd, seg.frg, seg.wnd, seg.ts, seg.sn, seg.una, len(seg.data), hdr)
	}
}
