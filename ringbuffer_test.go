package kcp

import "testing"

func TestRingBuffer(t *testing.T) {
	r := NewRingBuffer[int](1)
	for i := 0; i < 64; i++ {
		r.Push(i)
		if r.Len() != i+1 {
			t.Errorf("Expected length %d, got %d", i+1, r.Len())
		}
	}

	for i := 0; i < 32; i++ {
		val, ok := r.Pop()
		if !ok || val != i {
			t.Errorf("Expected to pop %d, got %d (ok: %v)", i, val, ok)
		}
	}

	if r.Len() != 32 {
		t.Errorf("Expected length 32 after popping 32 elements, got %d", r.Len())
	}

	// Push more elements to test the ring's behavior
	size := r.Len()
	for i := 0; i < 32; i++ {
		r.Push(i)
		if r.Len() != i+1+size {
			t.Errorf("Expected length %d, got %d", i+1+size, r.Len())
		}
	}

	if r.Len() != 64 {
		t.Errorf("Expected length 64 after pushing 32 more elements, got %d", r.Len())
	}
}
