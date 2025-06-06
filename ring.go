package kcp

import "fmt"

type Ring[T any] struct {
	head     int
	tail     int
	elements []T
}

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

func (r *Ring[T]) Len() int {
	if r.head <= r.tail {
		return r.tail - r.head
	}
	return len(r.elements) - r.head + r.tail
}

func (r *Ring[T]) Push(v T) {
	if r.IsFull() {
		r.grow()
	}
	r.elements[r.tail] = v
	r.tail = (r.tail + 1) % len(r.elements)
}

func (r *Ring[T]) Pop() (T, bool) {
	var zero T
	if r.Len() == 0 {
		return zero, false
	}
	value := r.elements[r.head]
	// clear is not necessary for generic type unless it's a pointer, but we assign zero value just in case
	r.elements[r.head] = zero
	r.head = (r.head + 1) % len(r.elements)
	return value, true
}

func (r *Ring[T]) Peek() (T, bool) {
	var zero T
	if r.Len() == 0 {
		return zero, false
	}
	return r.elements[r.head], true
}

func (r *Ring[T]) Clear() {
	r.head = 0
	r.tail = 0
	r.elements = make([]T, 0, 8)
}

func (r *Ring[T]) IsEmpty() bool {
	return r.Len() == 0
}

func (r *Ring[T]) Size() int {
	return len(r.elements)
}

func (r *Ring[T]) IsFull() bool {
	return (r.tail+1)%len(r.elements) == r.head
}

func (r *Ring[T]) grow() {
	currentSize := len(r.elements)
	var newSize int

	switch {
	case currentSize < 8:
		newSize = 8
	case currentSize <= 4096:
		newSize = currentSize * 2
	default:
		newSize = currentSize + (currentSize+9)/10 // +10% with ceiling
	}

	newElements := make([]T, newSize)

	if r.head < r.tail {
		copy(newElements, r.elements[r.head:r.tail])
	} else {
		n := copy(newElements, r.elements[r.head:])
		copy(newElements[n:], r.elements[:r.tail])
	}

	r.elements = newElements
	r.head = 0
	r.tail = r.Len()

	fmt.Println("growed to size:", newSize, "head:", r.head, "tail:", r.tail)
}
