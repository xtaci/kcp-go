package kcp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"io"
	"log"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

const cryptKey = "testkey"
const cryptSalt = "kcptest"

func TestAES(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 32, sha1.New)
	bc, err := NewAESBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
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

func TestTEA(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 16, sha1.New)
	bc, err := NewTEABlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
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

func TestSimpleXOR(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 32, sha1.New)
	bc, err := NewSimpleXORBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, mtuLimit)
	enc := make([]byte, mtuLimit)
	bc.Encrypt(enc, data)
	bc.Decrypt(dec, enc)
	if !bytes.Equal(data, dec) {
		log.Println(data)
		log.Println(dec)
		t.Fail()
	}
}

func TestBlowfish(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 32, sha1.New)
	bc, err := NewBlowfishBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
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

func TestNone(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 32, sha1.New)
	bc, err := NewNoneBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
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

func TestCast5(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 16, sha1.New)
	bc, err := NewCast5BlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
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

func TestTripleDES(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 24, sha1.New)
	bc, err := NewTripleDESBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
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
