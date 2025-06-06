package kcp

import "testing"

func TestRingSize(t *testing.T) {
	r := NewRingBuffer[int](1)
	if r.Len() != 0 {
		t.Errorf("Expected length 0, got %d", r.Len())
	}

	// re-zero
	for i := 0; i < 64; i++ {
		r.Push(i)
		r.Pop()
	}
	if r.Len() != 0 {
		t.Errorf("Expected length 0 after pushing and popping, got %d", r.Len())
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
		capacity := r.Capacity()
		valid := false
		for _, ec := range expectedCapacities {
			if capacity == ec {
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
	if r.Capacity() != 4506 {
		t.Errorf("expected capacity to be 4506 after pushing 4096, got %d", r.Capacity())
	}

	// push to 4506
	for i := 4097; i <= 4506; i++ {
		r.Push(i)
	}
	if r.Capacity() != 4957 {
		t.Errorf("expected capacity to be 4957 after pushing 4506, got %d", r.Capacity())
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
