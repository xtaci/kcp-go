// Optimized KCP segment header encoding/decoding.
//
// Uses BCE (Bounds Check Elimination) hints with individual field reads/writes,
// combined with a compile-time struct size assertion to guarantee correctness.
// On amd64/arm64 with Go 1.24+, binary.LittleEndian.UintXX() compiles to bare
// MOV instructions; the BCE hint ensures the slice bounds check happens exactly
// once per encode/decode call, and individual field operations allow the CPU
// pipeline to schedule independent memory operations in parallel.

package kcp

import (
	"encoding/binary"
	"unsafe"
)

// segmentHeader is the wire format of a KCP segment header (24 bytes).
// The struct layout exactly matches the KCP protocol wire format with no padding.
//
//	offset 0:  conv   (uint32)  - conversation id
//	offset 4:  cmd    (uint8)   - command
//	offset 5:  frg    (uint8)   - fragment count
//	offset 6:  wnd    (uint16)  - window size
//	offset 8:  ts     (uint32)  - timestamp
//	offset 12: sn     (uint32)  - sequence number
//	offset 16: una    (uint32)  - unacknowledged sequence number
//	offset 20: length (uint32)  - data length
type segmentHeader struct {
	conv   uint32
	cmd    uint8
	frg    uint8
	wnd    uint16
	ts     uint32
	sn     uint32
	una    uint32
	length uint32
}

// Compile-time assertion: segmentHeader must be exactly IKCP_OVERHEAD bytes.
var _ [0]struct{} = [unsafe.Sizeof(segmentHeader{}) - IKCP_OVERHEAD]struct{}{}
var _ [0]struct{} = [IKCP_OVERHEAD - unsafe.Sizeof(segmentHeader{})]struct{}{}

// decodeSegHeader decodes a KCP segment header from data into h.
// Caller MUST ensure len(data) >= IKCP_OVERHEAD.
//
// Uses a pointer receiver to write fields directly to the caller's struct,
// eliminating the struct copy overhead that occurs with value returns.
// Combined with a BCE hint, all bounds checks are eliminated after a single
// length check, and the CPU pipeline can schedule the independent loads
// in parallel.
func decodeSegHeader(data []byte, h *segmentHeader) {
	_ = data[IKCP_OVERHEAD-1] // BCE hint: one check eliminates all subsequent bounds checks
	h.conv = binary.LittleEndian.Uint32(data)
	h.cmd = data[4]
	h.frg = data[5]
	h.wnd = binary.LittleEndian.Uint16(data[6:])
	h.ts = binary.LittleEndian.Uint32(data[8:])
	h.sn = binary.LittleEndian.Uint32(data[12:])
	h.una = binary.LittleEndian.Uint32(data[16:])
	h.length = binary.LittleEndian.Uint32(data[20:])
}

// encodeSegHeader encodes a KCP segment header into ptr.
// Caller MUST ensure len(ptr) >= IKCP_OVERHEAD.
//
// Takes segmentHeader by value so the compiler can keep fields in registers
// (the struct is 24 bytes, fitting partially in registers on amd64).
// Uses BCE hint + individual field writes for optimal performance on modern
// out-of-order CPUs.
func encodeSegHeader(ptr []byte, h segmentHeader) {
	_ = ptr[IKCP_OVERHEAD-1] // BCE hint
	binary.LittleEndian.PutUint32(ptr, h.conv)
	ptr[4] = h.cmd
	ptr[5] = h.frg
	binary.LittleEndian.PutUint16(ptr[6:], h.wnd)
	binary.LittleEndian.PutUint32(ptr[8:], h.ts)
	binary.LittleEndian.PutUint32(ptr[12:], h.sn)
	binary.LittleEndian.PutUint32(ptr[16:], h.una)
	binary.LittleEndian.PutUint32(ptr[20:], h.length)
}
