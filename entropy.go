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
	"crypto/cipher"
	crand "crypto/rand"
	"io"
	"math/rand/v2"
	"runtime"
	"sync"

	"golang.org/x/sys/cpu"
)

const reseedInterval = 1 << 24

var (
	hasAESAsmAMD64 = cpu.X86.HasAES && cpu.X86.HasSSE41 && cpu.X86.HasSSSE3
	hasAESAsmARM64 = cpu.ARM64.HasAES
	hasAESAsmS390X = cpu.S390X.HasAES
	hasAESAsmPPC64 = runtime.GOARCH == "ppc64" || runtime.GOARCH == "ppc64le"

	hasAESHardwareSupport = hasAESAsmAMD64 || hasAESAsmARM64 || hasAESAsmS390X || hasAESAsmPPC64

	entropy io.Reader = NewEntropy()
)

func NewEntropy() io.Reader {
	if hasAESHardwareSupport {
		return NewEntropyAES()
	}

	return NewEntropyChacha8()
}

func SetEntropy(r io.Reader) {
	entropy = r
}

func fillRand(p []byte) {
	if len(p) <= 0 {
		return
	}
	io.ReadFull(entropy, p)
}

type rngAES struct {
	mutex sync.Mutex
	block cipher.Block
	seed  [16]byte
	count uint64
}

func NewEntropyAES() io.Reader {
	r := new(rngAES)

	var key [16]byte
	io.ReadFull(crand.Reader, key[:])
	io.ReadFull(crand.Reader, r.seed[:])

	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	r.block = block
	return r
}

func (r *rngAES) reseed() {
	if r.count < reseedInterval {
		r.count++
		return
	}

	var key [16]byte
	io.ReadFull(crand.Reader, key[:])
	io.ReadFull(crand.Reader, r.seed[:])

	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	r.block = block
	r.count = 0
}

func (r *rngAES) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.reseed()
	r.block.Encrypt(r.seed[:], r.seed[:])
	return copy(p, r.seed[:]), nil
}

type rngChacha8 struct {
	mutex sync.Mutex
	rand  *rand.ChaCha8
	count uint64
}

func NewEntropyChacha8() io.Reader {
	var seed [32]byte
	io.ReadFull(crand.Reader, seed[:])

	return &rngChacha8{
		rand: rand.NewChaCha8(seed),
	}
}

func (r *rngChacha8) reseed() {
	if r.count < reseedInterval {
		r.count++
		return
	}

	var seed [32]byte
	io.ReadFull(crand.Reader, seed[:])

	r.rand.Seed(seed)
	r.count = 0
}

func (r *rngChacha8) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.reseed()
	n, err := r.rand.Read(p)
	return n, err
}
