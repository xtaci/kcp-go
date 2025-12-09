package kcp

import "testing"

func TestBufferPoolGetSize(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	buf := bp.Get()

	// Check length
	if len(buf) != mtuLimit {
		t.Fatalf("expected len=%d, got %d", mtuLimit, len(buf))
		return
	}

	// Check capacity
	if cap(buf) != mtuLimit {
		t.Fatalf("expected cap=%d, got %d", mtuLimit, cap(buf))
		return
	}
}

func TestBufferPoolPutAndReuse(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	buf := bp.Get()
	// Modify buffer to track it
	buf[0] = 99

	// Put back to pool
	bp.Put(buf)

	// Get again; it should reuse the same buffer
	buf2 := bp.Get()

	// Check if it is reused by comparing pointer address
	if &buf2[0] != &buf[0] {
		t.Fatalf("expected buffer reuse, but got a new one")
		return
	}

	if buf2[0] != 99 {
		t.Fatalf("expected reused buffer to keep previous data")
		return
	}
}

func TestBufferPoolPutWrongSizeIgnored(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	// Make a buffer with wrong capacity
	wrongBuf := make([]byte, 100)

	bp.Put(wrongBuf)

	// Get should still return a buffer with mtuLimit capacity
	buf := bp.Get()

	if cap(buf) != mtuLimit {
		t.Fatalf("pool accepted wrong-sized buffer; expected cap=%d, got %d", mtuLimit, cap(buf))
		return
	}
}
