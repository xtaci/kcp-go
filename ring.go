package kcp

import "fmt"

type Ring struct {
	head     int
	tail     int
	elements []interface{}
}

func NewRing(size int) *Ring {
	if size <= 8 {
		size = 8 // Ensure a minimum size
	}
	return &Ring{
		head:     0,
		tail:     0,
		elements: make([]interface{}, size),
	}
}

func (r *Ring) Len() int {
	if r.head <= r.tail {
		return r.tail - r.head
	}
	return len(r.elements) - r.head + r.tail
}

func (r *Ring) Push(v interface{}) {
	if r.IsFull() {
		r.grow()
	}

	r.elements[r.tail] = v
	r.tail = (r.tail + 1) % len(r.elements)
}

func (r *Ring) Pop() (interface{}, bool) {
	if r.Len() == 0 {
		return nil, false
	}

	value := r.elements[r.head]
	r.elements[r.head] = nil // Clear the value to avoid memory leak
	r.head = (r.head + 1) % len(r.elements)
	return value, true
}

func (r *Ring) Peek() interface{} {
	if r.Len() == 0 {
		return nil
	}
	return r.elements[r.head]
}

func (r *Ring) Clear() {
	r.head = 0
	r.tail = 0
	r.elements = make([]interface{}, 0, 8)
}

func (r *Ring) IsEmpty() bool {
	return r.Len() == 0
}

func (r *Ring) Size() int {
	return len(r.elements)
}

func (r *Ring) IsFull() bool {
	if (r.tail+1)%len(r.elements) == r.head {
		return true
	}
	return false
}

// Grow increases the size of the ring buffer if it is full.
// It copies the existing elements to a new larger slice.
// If the ring is empty, it initializes it with a minimum size.
func (r *Ring) grow() {
	newSize := len(r.elements) * 2
	if newSize == 0 {
		newSize = 8 // Minimum size
	}
	newElements := make([]interface{}, newSize)

	if r.head < r.tail {
		// | ... head ..... tail ... |
		copy(newElements, r.elements[r.head:r.tail])
	} else {
		// elements:    |... tail head ... |
		// newElements: | head ..... tail 00000000 |
		copy(newElements, r.elements[r.head:])
		copy(newElements[len(r.elements)-r.head:], r.elements[:r.tail])
	}

	r.elements = newElements
	r.head = 0
	r.tail = r.Len()

	fmt.Println("growed to size:", newSize, "head:", r.head, "tail:", r.tail)
}
