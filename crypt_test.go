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
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestSM4(t *testing.T) {
	bc, err := NewSM4BlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestAES(t *testing.T) {
	bc, err := NewAESBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestTEA(t *testing.T) {
	bc, err := NewTEABlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestXOR(t *testing.T) {
	bc, err := NewSimpleXORBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestBlowfish(t *testing.T) {
	bc, err := NewBlowfishBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestNone(t *testing.T) {
	bc, err := NewNoneBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestCast5(t *testing.T) {
	bc, err := NewCast5BlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func Test3DES(t *testing.T) {
	bc, err := NewTripleDESBlockCrypt(pass[:24])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestTwofish(t *testing.T) {
	bc, err := NewTwofishBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestXTEA(t *testing.T) {
	bc, err := NewXTEABlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func TestSalsa20(t *testing.T) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

func cryptTest(t *testing.T, bc BlockCrypt) {
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, mtuLimit)
	enc := make([]byte, mtuLimit)
	bc.Encrypt(enc, data)
	bc.Decrypt(dec, enc)
	if !bytes.Equal(data, dec) {
		t.Fail()
	}
}

func TestAES256GCM(t *testing.T) {
	bc, err := NewAESGCMCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}

	testAEAD(t, bc)
}

func TestAES128GCM(t *testing.T) {
	bc, err := NewAESGCMCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}

	testAEAD(t, bc)
}

func testAEAD(t *testing.T, bc BlockCrypt) {
	aead := bc.(*aeadCrypt)

	nonceSize := aead.NonceSize()

	size := mtuLimit - cryptHeaderSize - aead.Overhead()
	data := make([]byte, size)
	io.ReadFull(rand.Reader, data)

	// if the size of packet is cannot accommodate the AEAD overhead
	// Open and Seal will allocate a new slice internally, we need to
	// ensure that it does not happen for our MTU sized packets.
	packet := make([]byte, mtuLimit)

	// Seal
	dst := packet[:nonceSize]
	nonce := packet[:nonceSize]
	fillRand(nonce)

	sealedPacket := aead.Seal(dst, nonce, data, nil)
	if &sealedPacket[0] != &packet[0] {
		t.Fatal("Seal created a new slice")
		return
	}

	// Open
	dst = sealedPacket[:nonceSize]
	nonce = sealedPacket[:nonceSize]
	ciphertext := sealedPacket[nonceSize:]

	decrypted, err := aead.Open(dst, nonce, ciphertext, nil)
	if &decrypted[0] != &sealedPacket[0] {
		t.Fatal("Open created a new slice")
		return
	}

	if err != nil {
		t.Fatal(err)
		return
	}

	if !bytes.Equal(data, decrypted[nonceSize:]) {
		t.Fail()
	}
}

func BenchmarkSM4(b *testing.B) {
	bc, err := NewSM4BlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkAES128(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}

	benchCrypt(b, bc)
}

func BenchmarkAES192(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:24])
	if err != nil {
		b.Fatal(err)
		return
	}

	benchCrypt(b, bc)
}

func BenchmarkAES256(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}

	benchCrypt(b, bc)
}

func BenchmarkTEA(b *testing.B) {
	bc, err := NewTEABlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkXOR(b *testing.B) {
	bc, err := NewSimpleXORBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkBlowfish(b *testing.B) {
	bc, err := NewBlowfishBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkNone(b *testing.B) {
	bc, err := NewNoneBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkCast5(b *testing.B) {
	bc, err := NewCast5BlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func Benchmark3DES(b *testing.B) {
	bc, err := NewTripleDESBlockCrypt(pass[:24])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkTwofish(b *testing.B) {
	bc, err := NewTwofishBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func BenchmarkXTEA(b *testing.B) {
	bc, err := NewXTEABlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func BenchmarkSalsa20(b *testing.B) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

func benchCrypt(b *testing.B, bc BlockCrypt) {
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, mtuLimit)
	enc := make([]byte, mtuLimit)

	b.ReportAllocs()
	b.SetBytes(int64(len(enc) * 2))

	for b.Loop() {
		bc.Encrypt(enc, data)
		bc.Decrypt(dec, enc)
	}
}

func BenchmarkCRC32(b *testing.B) {
	content := make([]byte, 1024)
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		crc32.ChecksumIEEE(content)
	}
}

func BenchmarkCFB_AES_128_CRC32(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	for b.Loop() {
		checksum := crc32.ChecksumIEEE(data[cryptHeaderSize:])
		binary.LittleEndian.PutUint32(data[nonceSize:cryptHeaderSize], checksum)
		bc.Encrypt(data, data)
	}
}

func BenchmarkAEAD_AES_128_GCM(b *testing.B) {
	block, err := aes.NewCipher(pass[:16])
	if err != nil {
		panic(err)
		return
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
		return
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	nonce := data[:aead.NonceSize()]
	plaintext := data[aead.NonceSize():]

	for b.Loop() {
		aead.Seal(plaintext[:0], nonce, plaintext, nil)
	}
}

func BenchmarkCFB_Salsa20_CRC32(b *testing.B) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	for b.Loop() {
		checksum := crc32.ChecksumIEEE(data[cryptHeaderSize:])
		binary.LittleEndian.PutUint32(data[nonceSize:cryptHeaderSize], checksum)
		bc.Encrypt(data, data)
	}
}

func BenchmarkAEAD_Chacha20_Poly1035(b *testing.B) {
	aead, err := chacha20poly1305.New(pass[:32])
	if err != nil {
		panic(err)
		return
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	nonce := data[:aead.NonceSize()]
	plaintext := data[aead.NonceSize():]

	for b.Loop() {
		aead.Seal(plaintext[:0], nonce, plaintext, nil)
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
