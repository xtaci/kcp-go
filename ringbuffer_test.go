package kcp

import (
	"math/rand"
	"testing"
)

func TestRingSize(t *testing.T) {
	r := NewRingBuffer[int](1)
	if r.Len() != 0 {
		t.Errorf("Expected length 0, got %d", r.Len())
	}

	// re-zero
	for i := 0; i < 64; i++ {
		r.Push(i)
		r.Pop()
		if r.Len() != 0 {
			t.Errorf("Expected length 0 after pushing and popping, got %d", r.Len())
		}
	}

	left := 1024 * 1024
	for i := 0; i < left; i++ {
		r.Push(i)
	}

	for {
		use := rand.Int() % (left + 1)
		left -= use
		for j := 0; j < use; j++ {
			if _, ok := r.Pop(); !ok {
				t.Errorf("Expected to pop value, but got none")
			}
		}
		if r.Len() != left {
			t.Errorf("Expected length %d after popping, got %d", left, r.Len())
		}

		t.Log("use", use, "left", left, "head", r.head, "tail", r.tail, "len", r.Len(), "maxlen", r.MaxLen())
		if left == 0 {
			break
		}
	}

	if r.Len() != 0 {
		t.Errorf("Expected length 0 after pushing and popping, got %d", r.Len())
	}
}

func TestRingSize2(t *testing.T) {
	// emulate a condition where head = 32， tail=31, and Len() = 63
	r := NewRingBuffer[int](64)
	for i := 0; i < 63; i++ {
		r.Push(i)
	}
	for i := 0; i < 32; i++ {
		if _, ok := r.Pop(); !ok {
			t.Errorf("Expected to pop value, but got none")
		}
	}
	for i := 0; i < 32; i++ {
		r.Push(i + 63)
		if r.Len() != i+(63-32+1) {
			t.Errorf("Expected length %d after popping 31 elements, got %d", i+(63-32+1), r.Len())
		}
	}

	// check head, tail and Len()
	if r.head != 32 {
		t.Errorf("Expected head to be 32, got %d", r.head)
	}
	if r.tail != 31 {
		t.Errorf("Expected tail to be 31, got %d", r.tail)
	}
	if r.Len() != 63 {
		t.Errorf("Expected length to be 63, got %d", r.Len())
	}

	t.Log("head", r.head, "tail", r.tail, "len", r.Len(), "maxlen", r.MaxLen())

	// now we push one element to trigger grow
	r.Push(95)
	if r.MaxLen() != 127 {
		t.Errorf("Expected capacity to be 128 after grow, got %d", r.MaxLen())
	}

	if r.Len() != 64 {
		t.Errorf("Expected length to be 64 after pushing one element, got %d", r.Len())
	}

	if r.head != 0 {
		t.Errorf("Expected head to be 0 after grow, got %d", r.head)
	}

	if r.tail != 64 {
		t.Errorf("Expected tail to be 64 after grow, got %d", r.tail)
	}
}

func TestRingBuffer(t *testing.T) {
	r := NewRingBuffer[int](1)
	if r.Len() != 0 {
		t.Errorf("Expected length 0, got %d", r.Len())
	}

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

func TestRingBufferGrow(t *testing.T) {
	initialSize := 4
	r := NewRingBuffer[int](initialSize)

	expectedCapacities := []int{8, 16, 32, 64, 128}

	// Push enough elements to trigger multiple grows
	pushCount := 100
	for i := 0; i < pushCount; i++ {
		r.Push(i)

		// Check if capacity is among expected
		capacity := r.MaxLen()
		valid := false
		for _, ec := range expectedCapacities {
			if capacity == ec-1 {
				valid = true
				break
			}
		}
		if !valid && capacity <= 4096 {
			t.Errorf("unexpected capacity during growth: %d", capacity)
		}
	}

	// Push to 4096
	for i := pushCount; i < 4096; i++ {
		r.Push(i)
	}

	// Make sure the growth is 10% increase after 4096
	r.Push(4096)
	if r.MaxLen() != 4505 {
		t.Errorf("expected capacity to be 4506, got %d", r.MaxLen())
	}

	// push to 4506
	for i := 4097; i <= 4505; i++ {
		r.Push(i)
	}
	if r.MaxLen() != 4956 {
		t.Errorf("expected capacity to be 4957, got %d", r.MaxLen())
	}

	// Check values are preserved in correct order
	for i := 0; i < pushCount; i++ {
		v, ok := r.Pop()
		if !ok {
			t.Fatalf("expected to pop value at index %d", i)
		}
		if v != i {
			t.Errorf("expected value %d, got %d", i, v)
		}
	}
}
