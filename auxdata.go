package kcp

import "container/heap"

// a heap struct to assist send queue management
type auxdata struct {
	sn uint32
	v  uint32 // value for heap comparsion
}

type auxHeap struct {
	s       []auxdata
	indices map[uint32]int // sn -> idx of sn
}

func newAuxHeap() *auxHeap {
	aux := new(auxHeap)
	aux.indices = make(map[uint32]int)
	return aux
}

func (h auxHeap) Len() int           { return len(h.s) }
func (h auxHeap) Less(i, j int) bool { return _itimediff(h.s[i].v, h.s[j].v) < 0 }
func (h auxHeap) Swap(i, j int) {
	h.s[i], h.s[j] = h.s[j], h.s[i]
	h.indices[h.s[i].sn] = i
	h.indices[h.s[j].sn] = j
}

func (h *auxHeap) Push(x interface{}) {
	h.s = append(h.s, x.(auxdata))
	i := len(h.s) - 1
	h.indices[h.s[i].sn] = i
}

func (h *auxHeap) Set(x auxdata) {
	if idx, ok := h.indices[x.sn]; ok {
		h.s[idx] = x
		heap.Fix(h, idx)
	} else {
		heap.Push(h, x)
	}
}

func (h *auxHeap) Pop() interface{} {
	n := len(h.s)
	x := h.s[n-1]
	h.s = h.s[0 : n-1]
	delete(h.indices, x.sn)
	return x
}
