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
	"sync"
	"testing"
)

func TestBufferPoolGetSize(t *testing.T) {
	bp := newPacketBufferPool()

	buf := bp.Get()

	// Check length
	if len(buf) != maxPacketSize {
		t.Fatalf("expected len=%d, got %d", maxPacketSize, len(buf))
		return
	}

	// Check capacity
	if cap(buf) != maxPacketSize {
		t.Fatalf("expected cap=%d, got %d", maxPacketSize, cap(buf))
		return
	}
}

func TestBufferPoolPutResetsLength(t *testing.T) {
	bp := newPacketBufferPool()

	buf := bp.Get()[:1]
	if err := bp.Put(buf); err != nil {
		t.Fatal(err)
	}
	buf2 := bp.Get()

	if len(buf2) != maxPacketSize {
		t.Fatalf("expected len=%d, got %d", maxPacketSize, len(buf2))
	}
	if cap(buf2) != maxPacketSize {
		t.Fatalf("expected cap=%d, got %d", maxPacketSize, cap(buf2))
	}
}

func TestBufferPoolPutWrongSizeIgnored(t *testing.T) {
	bp := newPacketBufferPool()

	// Make a buffer with wrong capacity
	wrongBuf := make([]byte, 100)

	bp.Put(wrongBuf)

	// Get should still return a buffer with maxPacketSize capacity
	buf := bp.Get()

	if cap(buf) != maxPacketSize {
		t.Fatalf("pool accepted wrong-sized buffer; expected cap=%d, got %d", maxPacketSize, cap(buf))
		return
	}
}

func TestBufferPoolPutReturnsError(t *testing.T) {
	bp := newPacketBufferPool()

	// 1. Correct size
	buf := make([]byte, maxPacketSize)
	if err := bp.Put(buf); err != nil {
		t.Fatalf("expected nil error for correct size, got %v", err)
	}

	// 2. Incorrect size
	wrongBuf := make([]byte, maxPacketSize+1)
	if err := bp.Put(wrongBuf); err == nil {
		t.Fatalf("expected error for wrong size, got nil")
	}
}

// BenchmarkPacketBufferPoolGetPut compares the production *[maxPacketSize]byte
// pool against a naive []byte pool to demonstrate the slice-header boxing
// allocation avoided on sync.Pool.Put.
func BenchmarkPacketBufferPoolGetPut(b *testing.B) {
	runPair := func(b *testing.B, get func() []byte, put func([]byte)) {
		b.Run("Serial", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buf := get()[:1]
				buf[0] = 1
				put(buf)
			}
		})
		b.Run("Parallel", func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					buf := get()[:1]
					buf[0] = 1
					put(buf)
				}
			})
		})
	}

	b.Run("ArrayPointer", func(b *testing.B) {
		bp := newPacketBufferPool()
		runPair(b, bp.Get, func(buf []byte) {
			_ = bp.Put(buf)
		})
	})

	b.Run("Slice", func(b *testing.B) {
		// Local baseline only; not for production use.
		var sp = sync.Pool{
			New: func() any {
				return make([]byte, maxPacketSize)
			},
		}
		runPair(b,
			func() []byte {
				return sp.Get().([]byte)
			},
			func(buf []byte) {
				if cap(buf) == maxPacketSize {
					sp.Put(buf[:maxPacketSize])
				}
			},
		)
	})
}
