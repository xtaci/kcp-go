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
	for i := range 64 {
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
	for i := range 63 {
		r.Push(i)
	}
	for range 32 {
		if _, ok := r.Pop(); !ok {
			t.Errorf("Expected to pop value, but got none")
		}
	}
	for i := range 32 {
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

	for i := range 64 {
		r.Push(i)
		if r.Len() != i+1 {
			t.Errorf("Expected length %d, got %d", i+1, r.Len())
		}
	}

	for i := range 32 {
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
	for i := range 32 {
		r.Push(i)
		if r.Len() != i+1+size {
			t.Errorf("Expected length %d, got %d", i+1+size, r.Len())
		}
	}

	if r.Len() != 64 {
		t.Errorf("Expected length 64 after pushing 32 more elements, got %d", r.Len())
	}

	// ringbuffer should be [32 ... 63, 0 ... 31]
	evicted := 0
	round := 0
	expectedHead := []int{62, 28}
	for !r.IsEmpty() {
		evicted += r.Discard(30)
		if round < len(expectedHead) {
			if v, ok := r.Peek(); !ok || *v != expectedHead[round] {
				t.Errorf("Invalid discard state: unexpected %d", *v)
			}
		} else if _, ok := r.Peek(); ok {
			t.Errorf("Unexpected non-nil head element")
		}
		if r.Len()+evicted != 64 {
			t.Errorf("Unexpected ringbuffer length after discard op")
		}
		round++
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
		if !valid && capacity <= RINGBUFFER_EXP {
			t.Errorf("unexpected capacity during growth: %d", capacity)
		}
	}

	// Push to 1024
	for i := pushCount; i < 1024; i++ {
		r.Push(i)
	}

	// Make sure the growth is 10% increase after 4096
	r.Push(1)
	if r.MaxLen() != 1126 {
		t.Errorf("expected capacity to be 1126, got %d", r.MaxLen())
	}

	// Check values are preserved in correct order
	for i := range pushCount {
		v, ok := r.Pop()
		if !ok {
			t.Fatalf("expected to pop value at index %d", i)
			return
		}
		if v != i {
			t.Errorf("expected value %d, got %d", i, v)
		}
	}
}

func TestRingForEach(t *testing.T) {
	r := NewRingBuffer[int](10)
	for i := 0; i < 10; i++ {
		r.Push(i)
	}

	sum := 0
	i := 0
	r.ForEach(func(v *int) bool {
		if *v != i {
			t.Errorf("Expected index %d, got %d", i, *v)
		}
		i++
		sum += *v
		return true
	})

	if sum != 45 { // 0 + 1 + ... + 9 = 45
		t.Errorf("Expected sum to be 45, got %d", sum)
	}

	count := 0
	r.ForEach(func(v *int) bool {
		count++
		return true
	})

	if count != 10 {
		t.Errorf("Expected count to be 10, got %d", count)
	}
}

func TestRingForEachReverse(t *testing.T) {
	r := NewRingBuffer[int](10)
	for i := range 10 {
		r.Push(i)
	}

	sum := 0
	i := 9
	r.ForEachReverse(func(v *int) bool {
		if *v != i {
			t.Errorf("Expected index %d, got %d", i, *v)
		}
		i--
		sum += *v
		return true
	})

	if sum != 45 { // 0 + 1 + ... + 9 = 45
		t.Errorf("Expected sum to be 45, got %d", sum)
	}

	count := 0
	r.ForEachReverse(func(v *int) bool {
		count++
		return true
	})

	if count != 10 {
		t.Errorf("Expected count to be 10, got %d", count)
	}
}
