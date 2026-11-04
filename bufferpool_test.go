package kcp

import "testing"

func TestBufferPoolGetSize(t *testing.T) {
	bp := newBufferPool(maxPacketSize)

	buf := bp.Get()

	// Check length
	if len(buf) != maxPacketSize {
		t.Fatalf("expected len=%d, got %d", maxPacketSize, len(buf))
		return
	}

	// Check capacity
	if cap(buf) != maxPacketSize {
		t.Fatalf("expected cap=%d, got %d", maxPacketSize, cap(buf))
		return
	}
}

func TestBufferPoolPutResetsLength(t *testing.T) {
	bp := newBufferPool(maxPacketSize)

	buf := bp.Get()[:1]
	if err := bp.Put(buf); err != nil {
		t.Fatal(err)
	}
	buf2 := bp.Get()

	if len(buf2) != maxPacketSize {
		t.Fatalf("expected len=%d, got %d", maxPacketSize, len(buf2))
	}
	if cap(buf2) != maxPacketSize {
		t.Fatalf("expected cap=%d, got %d", maxPacketSize, cap(buf2))
	}
}

func TestBufferPoolPutWrongSizeIgnored(t *testing.T) {
	bp := newBufferPool(maxPacketSize)

	// Make a buffer with wrong capacity
	wrongBuf := make([]byte, 100)

	bp.Put(wrongBuf)

	// Get should still return a buffer with maxPacketSize capacity
	buf := bp.Get()

	if cap(buf) != maxPacketSize {
		t.Fatalf("pool accepted wrong-sized buffer; expected cap=%d, got %d", maxPacketSize, cap(buf))
		return
	}
}

func TestBufferPoolPutReturnsError(t *testing.T) {
	bp := newBufferPool(maxPacketSize)

	// 1. Correct size
	buf := make([]byte, maxPacketSize)
	if err := bp.Put(buf); err != nil {
		t.Fatalf("expected nil error for correct size, got %v", err)
	}

	// 2. Incorrect size
	wrongBuf := make([]byte, maxPacketSize+1)
	if err := bp.Put(wrongBuf); err == nil {
		t.Fatalf("expected error for wrong size, got nil")
	}
}
