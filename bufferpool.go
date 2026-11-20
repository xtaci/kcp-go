// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package kcp

import (
	"errors"
	"sync"
)

// pre-allocated error to avoid repeated allocations
var errBufferSizeMismatch = errors.New("buffer size mismatch")

// A system-wide packet buffer shared among sending, receiving and FEC
// to mitigate high-frequency memory allocation of packets.
var defaultBufferPool = newPacketBufferPool()

type packetBuffer = [maxPacketSize]byte

type packetBufferPool struct {
	xmitBuf sync.Pool
}

// newPacketBufferPool creates a new packet buffer pool with fixed maxPacketSize buffers.
func newPacketBufferPool() *packetBufferPool {
	return &packetBufferPool{
		xmitBuf: sync.Pool{
			New: func() any {
				return new(packetBuffer)
			},
		},
	}
}

// Get retrieves a buffer from the pool.
func (bp *packetBufferPool) Get() []byte {
	return (*bp.xmitBuf.Get().(*packetBuffer))[:]
}

// Put returns a buffer to the pool.
func (bp *packetBufferPool) Put(buf []byte) error {
	// Only put back buffers whose capacity matches maxPacketSize.
	if cap(buf) != maxPacketSize {
		return errBufferSizeMismatch
	}
	bp.xmitBuf.Put((*packetBuffer)(buf[:maxPacketSize]))
	return nil
}

// Puts returns some buffers to the pool.
func (bp *packetBufferPool) Puts(buffers [][]byte) {
	for _, buf := range buffers {
		bp.Put(buf)
	}
}

// PutPkts returns some FEC packet buffers to the pool.
func (bp *packetBufferPool) PutPkts(buffers []fecPacket) {
	for _, buf := range buffers {
		bp.Put(buf)
	}
}
