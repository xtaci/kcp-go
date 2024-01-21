package kcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"io"
)

// Entropy defines a entropy source
type Entropy interface {
	Init()
	Fill(nonce []byte)
}

// nonceSHA256 nonce generator for packet header
type nonceSHA256 struct {
	seed [sha256.Size]byte
}

func (*nonceSHA256) Init() { /* nothing required */ }

func (n *nonceSHA256) Fill(nonce []byte) {
	if n.seed[0] == 0 { // entropy update
		io.ReadFull(rand.Reader, n.seed[:])
	}
	n.seed = sha256.Sum256(n.seed[:])
	copy(nonce, n.seed[:])
}

// nonceAES128 nonce generator for packet headers
type nonceAES128 struct {
	seed  [aes.BlockSize]byte
	block cipher.Block
}

func (n *nonceAES128) Init() {
	var key [16]byte // aes-128
	io.ReadFull(rand.Reader, key[:])
	io.ReadFull(rand.Reader, n.seed[:])
	block, _ := aes.NewCipher(key[:])
	n.block = block
}

func (n *nonceAES128) Fill(nonce []byte) {
	if n.seed[0] == 0 { // entropy update
		io.ReadFull(rand.Reader, n.seed[:])
	}
	n.block.Encrypt(n.seed[:], n.seed[:])
	copy(nonce, n.seed[:])
}
