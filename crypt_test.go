package kcp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"io"
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
	data := make([]byte, 4096)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, 4096)
	enc := make([]byte, 4096)
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
	data := make([]byte, 4096)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, 4096)
	enc := make([]byte, 4096)
	bc.Encrypt(enc, data)
	bc.Decrypt(dec, enc)
	if !bytes.Equal(data, dec) {
		t.Fail()
	}

}

func TestSimpleXOR(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 16, sha1.New)
	bc, err := NewSimpleXORBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4096)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, 4096)
	enc := make([]byte, 4096)
	bc.Encrypt(enc, data)
	bc.Decrypt(dec, enc)
	if !bytes.Equal(data, dec) {
		t.Fail()
	}
}

func TestBlowfish(t *testing.T) {
	pass := pbkdf2.Key(key, []byte(salt), 4096, 32, sha1.New)
	bc, err := NewBlowfishBlockCrypt(pass)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4096)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, 4096)
	enc := make([]byte, 4096)
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
	data := make([]byte, 4096)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, 4096)
	enc := make([]byte, 4096)
	bc.Encrypt(enc, data)
	bc.Decrypt(dec, enc)
	if !bytes.Equal(data, dec) {
		t.Fail()
	}
}
