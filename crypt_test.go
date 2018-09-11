package kcp

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"hash/crc32"
	"io"
	"sync"
	"testing"
)

func TestSM4(t *testing.T) {
	bc, err := NewSM4BlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestAES(t *testing.T) {
	bc, err := NewAESBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestTEA(t *testing.T) {
	bc, err := NewTEABlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestXOR(t *testing.T) {
	bc, err := NewSimpleXORBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestBlowfish(t *testing.T) {
	bc, err := NewBlowfishBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestNone(t *testing.T) {
	bc, err := NewNoneBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestCast5(t *testing.T) {
	bc, err := NewCast5BlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func Test3DES(t *testing.T) {
	bc, err := NewTripleDESBlockCrypt(pass[:24])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestTwofish(t *testing.T) {
	bc, err := NewTwofishBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestXTEA(t *testing.T) {
	bc, err := NewXTEABlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
	}
	cryptTest(t, bc)
}

func TestSalsa20(t *testing.T) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
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

func BenchmarkAheadChaCha20Poly1305(b *testing.B) {
	if bc, err := NewChacha20Ploy1305(pass[:32]); err != nil{
		b.Fatal(err)
	}else{
		benchAhead(b, bc, 4)
	}
}
func BenchmarkAES128GCM(b *testing.B) {
	if bc, err := NewAES128GCM(pass[:16]); err != nil{
		b.Fatal(err)
	}else{
		benchAhead(b, bc, 4)
	}
}
func BenchmarkAES192GCM(b *testing.B) {
	if bc, err := NewAES196GCM(pass[:24]); err != nil{
		b.Fatal(err)
	}else{
		benchAhead(b, bc, 4)
	}
}
func BenchmarkAES256GCM(b *testing.B) {
	if bc, err := NewAES256GCM(pass[:32]); err != nil{
		b.Fatal(err)
	}else{
		benchAhead(b, bc, 4)
	}
}


func goRoutingEncDec(b *testing.B, bc AheadCipher, workers chan bool, data []byte, mutex sync.Mutex){
	var err error
	dec := make([]byte, 2*mtuLimit)
	enc := make([]byte, 2*mtuLimit)
	for{
		select{
		case signal := <-workers:
			if signal {
				if enc, err = bc.Encrypt(enc, data); err != nil {
					b.Errorf("Ahead encryption failed: %s", err.Error())
					b.Fail()
					return
				}
				if dec, err = bc.Decrypt(dec, enc); err != nil {
					b.Errorf("Ahead dencryption failed: %s", err.Error())
					b.Fail()
					return
				}
				mutex.Lock()
				b.SetBytes(mtuLimit * 2)
				mutex.Unlock()
			}else{
				// quit
				return
			}
		}
	}
}
func benchAhead(b *testing.B, bc AheadCipher, parallel int) {
	b.ReportAllocs()
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)

	workers := make(chan bool, parallel)
	var mux sync.Mutex
	for c := 0; c < parallel; c++{
		go goRoutingEncDec(b, bc, workers, data, mux)
	}
	workerIdx := 0
	for i := 0; i < b.N; i++ {
		workers <- true
		workerIdx++
		if workerIdx >= parallel{
			workerIdx = 0
		}
	}

	for c := 0; c < parallel; c++{
		workers <- false
	}


}


func BenchmarkSM4(b *testing.B) {
	bc, err := NewSM4BlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func BenchmarkAES128(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
	}

	benchCrypt(b, bc)
}

func BenchmarkAES192(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:24])
	if err != nil {
		b.Fatal(err)
	}

	benchCrypt(b, bc)
}

func BenchmarkAES256(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}

	benchCrypt(b, bc)
}

func BenchmarkTEA(b *testing.B) {
	bc, err := NewTEABlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func BenchmarkXOR(b *testing.B) {
	bc, err := NewSimpleXORBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func BenchmarkBlowfish(b *testing.B) {
	bc, err := NewBlowfishBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func BenchmarkNone(b *testing.B) {
	bc, err := NewNoneBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func BenchmarkCast5(b *testing.B) {
	bc, err := NewCast5BlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func Benchmark3DES(b *testing.B) {
	bc, err := NewTripleDESBlockCrypt(pass[:24])
	if err != nil {
		b.Fatal(err)
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
	}
	benchCrypt(b, bc)
}

func BenchmarkSalsa20(b *testing.B) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

func benchCrypt(b *testing.B, bc BlockCrypt) {
	b.ReportAllocs()
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, mtuLimit)
	enc := make([]byte, mtuLimit)

	for i := 0; i < b.N; i++ {
		bc.Encrypt(enc, data)
		bc.Decrypt(dec, enc)
	}
	b.SetBytes(int64(len(enc) * 2))
}

func BenchmarkCRC32(b *testing.B) {
	content := make([]byte, 1024)
	b.SetBytes(int64(len(content)))
	for i := 0; i < b.N; i++ {
		crc32.ChecksumIEEE(content)
	}
}

func BenchmarkCsprngSystem(b *testing.B) {
	data := make([]byte, md5.Size)
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		io.ReadFull(rand.Reader, data)
	}
}

func BenchmarkCsprngMD5(b *testing.B) {
	var data [md5.Size]byte
	b.SetBytes(md5.Size)

	for i := 0; i < b.N; i++ {
		data = md5.Sum(data[:])
	}
}
func BenchmarkCsprngSHA1(b *testing.B) {
	var data [sha1.Size]byte
	b.SetBytes(sha1.Size)

	for i := 0; i < b.N; i++ {
		data = sha1.Sum(data[:])
	}
}

func BenchmarkCsprngMD5AndSystem(b *testing.B) {
	var ng nonceMD5

	b.SetBytes(md5.Size)
	data := make([]byte, md5.Size)
	for i := 0; i < b.N; i++ {
		ng.Fill(data)
	}
}


func TestAheadChaCha20Poly1305(t *testing.T){
	if bc, err := NewChacha20Ploy1305(pass[:32]); err != nil{
		t.Fatal(err)
	}else{
		aheadTest(t, bc)
	}
}

func TestAheadAES128GCM(t *testing.T){
	if bc, err := NewAES128GCM(pass[:16]); err != nil{
		t.Fatal(err)
	}else{
		aheadTest(t, bc)
	}
}
func TestAheadAES192GCM(t *testing.T){
	if bc, err := NewAES196GCM(pass[:24]); err != nil{
		t.Fatal(err)
	}else{
		aheadTest(t, bc)
	}
}

func TestAheadAES256GCM(t *testing.T){
	if bc, err := NewAES256GCM(pass[:32]); err != nil{
		t.Fatal(err)
	}else{
		aheadTest(t, bc)
	}
}


func aheadTest(t *testing.T, bc AheadCipher) {
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)

	var err error
	dec := make([]byte, 2 * mtuLimit)
	enc := make([]byte, 2 * mtuLimit)

	if enc, err = bc.Encrypt(enc, data); err != nil{
		t.Errorf("Ahead encryption failed: %s", err.Error())
		t.Fail()
	}

	if dec, err = bc.Decrypt(dec, enc); err != nil{
		t.Errorf("Ahead dencryption failed: %s", err.Error())
		t.Fail()
	}

	if !bytes.Equal(data, dec) {
		t.Fail()
	}
}

