package kcp

import "fmt"

// Ring is a generic ring (circular) buffer that supports dynamic resizing.
// It provides efficient FIFO queue behavior with amortized constant time operations.
type Ring[T any] struct {
	head     int   // Index of the next element to be popped
	tail     int   // Index of the next empty slot to push into
	elements []T   // Underlying slice storing elements in circular fashion
}

// NewRing creates a new Ring with a specified initial capacity.
// If the provided size is <= 8, it defaults to 8.
func NewRing[T any](size int) *Ring[T] {
	if size <= 8 {
		size = 8 // Ensure a minimum size
	}
	return &Ring[T]{
		head:     0,
		tail:     0,
		elements: make([]T, size),
	}
}

// Len returns the number of elements currently in the ring.
func (r *Ring[T]) Len() int {
	if r.head <= r.tail {
		return r.tail - r.head
	}
	return len(r.elements) - r.head + r.tail
}

// Push adds an element to the tail of the ring.
// If the ring is full, it will grow automatically.
func (r *Ring[T]) Push(v T) {
	if r.IsFull() {
		r.grow()
	}
	r.elements[r.tail] = v
	r.tail = (r.tail + 1) % len(r.elements)
}

// Pop removes and returns the element from the head of the ring.
// It returns the zero value and false if the ring is empty.
func (r *Ring[T]) Pop() (T, bool) {
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
func (r *Ring[T]) Peek() (T, bool) {
	var zero T
	if r.Len() == 0 {
		return zero, false
	}
	return r.elements[r.head], true
}

// Clear resets the ring to an empty state and reinitializes the buffer.
// The capacity is preserved.
func (r *Ring[T]) Clear() {
	r.head = 0
	r.tail = 0
	r.elements = make([]T, len(r.elements)) // Preserve current capacity
}

// IsEmpty returns true if the ring has no elements.
func (r *Ring[T]) IsEmpty() bool {
	return r.Len() == 0
}

// Size returns the current capacity of the ring buffer.
func (r *Ring[T]) Size() int {
	return len(r.elements)
}

// IsFull returns true if the ring buffer is full (tail + 1 == head).
func (r *Ring[T]) IsFull() bool {
	return (r.tail+1)%len(r.elements) == r.head
}

// grow increases the ring buffer's capacity when full.
// Growth policy:
//   - If current size < 8: grow to 8
//   - If size <= 4096: double the size
//   - If size > 4096: increase by 10% (rounded up)
func (r *Ring[T]) grow() {
	currentSize := len(r.elements)
	var newSize int

	switch {
	case currentSize < 8:
		newSize = 8
	case currentSize <= 4096:
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

	r.elements = newElements
	r.head = 0
	r.tail = r.Len()

	fmt.Println("growed to size:", newSize, "head:", r.head, "tail:", r.tail)
}
