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
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"io"
	"testing"
)

func TestEntropyAES(t *testing.T) {
	r := NewEntropyAES()
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 {
		t.Fatalf("expected 16 bytes, got %d", n)
	}

	// Test empty read
	n, err = r.Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

func TestEntropyChacha8(t *testing.T) {
	r := NewEntropyChacha8()
	buf := make([]byte, 32)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 32 {
		t.Fatalf("expected 32 bytes, got %d", n)
	}

	// Test empty read
	n, err = r.Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

func TestSetEntropy(t *testing.T) {
	// Save original entropy
	orig := entropy
	defer SetEntropy(orig)

	// Set custom entropy
	custom := NewEntropyChacha8()
	SetEntropy(custom)

	if entropy != custom {
		t.Fatal("SetEntropy failed")
	}

	// Test fillRand with custom entropy
	buf := make([]byte, 10)
	fillRand(buf)
	// We can't easily verify that the custom source provided the data, but we
	// can verify that the call did not panic.
}

func TestFillRand(t *testing.T) {
	buf := make([]byte, 100)
	fillRand(buf)

	// Check if buffer is not all zeros (probabilistic, but highly likely)
	allZero := true
	for _, b := range buf {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("fillRand produced all zeros")
	}

	// Test empty buffer
	fillRand(nil)
}

type mockReader struct{}

func (m *mockReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0xAA
	}
	return len(p), nil
}

func TestSetEntropyMock(t *testing.T) {
	orig := entropy
	defer SetEntropy(orig)

	SetEntropy(&mockReader{})
	buf := make([]byte, 10)
	fillRand(buf)
	for _, b := range buf {
		if b != 0xAA {
			t.Fatalf("expected 0xAA, got %x", b)
		}
	}
}

func BenchmarkCsprngSystem(b *testing.B) {
	data := make([]byte, md5.Size)
	b.SetBytes(int64(len(data)))

	for b.Loop() {
		io.ReadFull(rand.Reader, data)
	}
}

func BenchmarkCsprngAES128(b *testing.B) {
	var data [aes.BlockSize]byte
	b.SetBytes(aes.BlockSize)

	r := NewEntropyAES()
	for b.Loop() {
		io.ReadFull(r, data[:])
	}
}

func BenchmarkCsprngChacha8(b *testing.B) {
	var data [8]byte
	b.SetBytes(8)

	r := NewEntropyChacha8()
	for b.Loop() {
		io.ReadFull(r, data[:])
	}
}
