package kcp

const (
	RINGBUFFER_MIN = 8
	RINGBUFFER_EXP = 4096
)

// RingBuffer is a generic ring (circular) buffer that supports dynamic resizing.
// It provides efficient FIFO queue behavior with amortized constant time operations.
type RingBuffer[T any] struct {
	head     int // Index of the next element to be popped
	tail     int // Index of the next empty slot to push into
	elements []T // Underlying slice storing elements in circular fashion
}

// NewRingBuffer creates a new Ring with a specified initial capacity.
// If the provided size is <= 8, it defaults to 8.
func NewRingBuffer[T any](size int) *RingBuffer[T] {
	if size <= RINGBUFFER_MIN {
		size = RINGBUFFER_MIN // Ensure a minimum size
	}
	return &RingBuffer[T]{
		head:     0,
		tail:     0,
		elements: make([]T, size),
	}
}

// Len returns the number of elements currently in the ring.
func (r *RingBuffer[T]) Len() int {
	if r.head <= r.tail {
		return r.tail - r.head
	}

	return len(r.elements[r.head:]) + len(r.elements[:r.tail])
}

// Push adds an element to the tail of the ring.
// If the ring is full, it will grow automatically.
func (r *RingBuffer[T]) Push(v T) {
	if r.IsFull() {
		r.grow()
	}
	r.elements[r.tail] = v
	r.tail = (r.tail + 1) % len(r.elements)
}

// Pop removes and returns the element from the head of the ring.
// It returns the zero value and false if the ring is empty.
func (r *RingBuffer[T]) Pop() (T, bool) {
	var zero T
	if r.Len() == 0 {
		return zero, false
	}
	value := r.elements[r.head]
	// Optional: clear the slot to avoid retaining references
	r.elements[r.head] = zero
	r.head = (r.head + 1) % len(r.elements)
	return value, true
}

// Peek returns the element at the head of the ring without removing it.
// It returns the zero value and false if the ring is empty.
func (r *RingBuffer[T]) Peek() (T, bool) {
	var zero T
	if r.Len() == 0 {
		return zero, false
	}
	return r.elements[r.head], true
}

// ForEach iterates over each element in the ring buffer,
// applying the provided function. If the function returns false,
// iteration stops early.
func (r *RingBuffer[T]) ForEach(fn func(T) bool) {
	if r.Len() == 0 {
		return
	}
	if r.head < r.tail {
		// Contiguous data: [head ... tail)
		for i := r.head; i < r.tail; i++ {
			if !fn(r.elements[i]) {
				break // Stop iteration if function returns false
			}
		}
	} else {
		// Wrapped data: [head ... end) + [0 ... tail)
		for i := r.head; i < len(r.elements); i++ {
			if !fn(r.elements[i]) {
				break // Stop iteration if function returns false
			}
		}
		for i := 0; i < r.tail; i++ {
			if !fn(r.elements[i]) {
				break // Stop iteration if function returns false
			}
		}
	}
}

// Clear resets the ring to an empty state and reinitializes the buffer.
// The capacity is preserved.
func (r *RingBuffer[T]) Clear() {
	r.head = 0
	r.tail = 0
	r.elements = make([]T, RINGBUFFER_MIN)
}

// IsEmpty returns true if the ring has no elements.
func (r *RingBuffer[T]) IsEmpty() bool {
	return r.Len() == 0
}

// MaxLen returns the maximum capacity of the ring buffer.
func (r *RingBuffer[T]) MaxLen() int {
	return len(r.elements) - 1
}

// IsFull returns true if the ring buffer is full (tail + 1 == head).
func (r *RingBuffer[T]) IsFull() bool {
	return (r.tail+1)%len(r.elements) == r.head
}

// grow increases the ring buffer's capacity when full.
// Growth policy:
//   - If current size < 8: grow to 8
//   - If size <= 4096: double the size
//   - If size > 4096: increase by 10% (rounded up)
func (r *RingBuffer[T]) grow() {
	currentLength := r.Len()
	currentSize := len(r.elements)
	var newSize int

	switch {
	case currentSize < RINGBUFFER_MIN:
		newSize = RINGBUFFER_MIN
	case currentSize < RINGBUFFER_EXP:
		newSize = currentSize * 2
	default:
		newSize = currentSize + (currentSize+9)/10 // +10%, rounded up
	}

	newElements := make([]T, newSize)

	// Copy elements to new buffer preserving logical order
	if r.head < r.tail {
		// Contiguous data: [head ... tail)
		copy(newElements, r.elements[r.head:r.tail])
	} else {
		// Wrapped data: [head ... end) + [0 ... tail)
		n := copy(newElements, r.elements[r.head:])
		copy(newElements[n:], r.elements[:r.tail])
	}

	r.head = 0
	r.tail = currentLength
	r.elements = newElements
}
