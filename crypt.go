package kcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/sha1"

	"golang.org/x/crypto/blowfish"
	"golang.org/x/crypto/cast5"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/salsa20"
	"golang.org/x/crypto/tea"
	"golang.org/x/crypto/twofish"
	"golang.org/x/crypto/xtea"
)

var (
	initialVector = []byte{167, 115, 79, 156, 18, 172, 27, 1, 164, 21, 242, 193, 252, 120, 230, 107}
	saltxor       = `sH3CIVoF#rWLtJo6`
)

// BlockCrypt defines encryption/decryption methods for a given byte slice
type BlockCrypt interface {
	// Encrypt encrypts the whole block in src into dst.
	// Dst and src may point at the same memory.
	Encrypt(dst, src []byte)

	// Decrypt decrypts the whole block in src into dst.
	// Dst and src may point at the same memory.
	Decrypt(dst, src []byte)
}

type salsa20BlockCrypt struct {
	key [32]byte
}

// NewSalsa20BlockCrypt initates BlockCrypt by the given key
func NewSalsa20BlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(salsa20BlockCrypt)
	copy(c.key[:], key)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *salsa20BlockCrypt) Encrypt(dst, src []byte) {
	salsa20.XORKeyStream(dst[8:], src[8:], src[:8], &c.key)
	copy(dst[:8], src[:8])
}

// Decrypt implements Decrypt interface
func (c *salsa20BlockCrypt) Decrypt(dst, src []byte) {
	salsa20.XORKeyStream(dst[8:], src[8:], src[:8], &c.key)
	copy(dst[:8], src[:8])
}

// twofishBlockCrypt implements BlockCrypt
type twofishBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewTwofishBlockCrypt initates BlockCrypt by the given key
func NewTwofishBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(twofishBlockCrypt)
	block, err := twofish.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, twofish.BlockSize)
	c.decbuf = make([]byte, 2*twofish.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *twofishBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *twofishBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// tripleDESBlockCrypt implements BlockCrypt
type tripleDESBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewTripleDESBlockCrypt initates BlockCrypt by the given key
func NewTripleDESBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(tripleDESBlockCrypt)
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, des.BlockSize)
	c.decbuf = make([]byte, 2*des.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *tripleDESBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *tripleDESBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// cast5BlockCrypt implements BlockCrypt
type cast5BlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewCast5BlockCrypt initates BlockCrypt by the given key
func NewCast5BlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(cast5BlockCrypt)
	block, err := cast5.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, cast5.BlockSize)
	c.decbuf = make([]byte, 2*cast5.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *cast5BlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *cast5BlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// blowfishBlockCrypt implements BlockCrypt
type blowfishBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewBlowfishBlockCrypt initates BlockCrypt by the given key
func NewBlowfishBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(blowfishBlockCrypt)
	block, err := blowfish.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, blowfish.BlockSize)
	c.decbuf = make([]byte, 2*blowfish.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *blowfishBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *blowfishBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// aesBlockCrypt implements BlockCrypt
type aesBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewAESBlockCrypt initates BlockCrypt by the given key
func NewAESBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(aesBlockCrypt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, aes.BlockSize)
	c.decbuf = make([]byte, 2*aes.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *aesBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *aesBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// teaBlockCrypt implements BlockCrypt
type teaBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewTEABlockCrypt initate BlockCrypt by the given key
func NewTEABlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(teaBlockCrypt)
	block, err := tea.NewCipherWithRounds(key, 16)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, tea.BlockSize)
	c.decbuf = make([]byte, 2*tea.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *teaBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *teaBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// xteaABlockCrypt implements BlockCrypt
type xteaBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewXTEABlockCrypt initate BlockCrypt by the given key
func NewXTEABlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(xteaBlockCrypt)
	block, err := xtea.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, xtea.BlockSize)
	c.decbuf = make([]byte, 2*xtea.BlockSize)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *xteaBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }

// Decrypt implements Decrypt interface
func (c *xteaBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

// simpleXORBlockCrypt implements BlockCrypt
type simpleXORBlockCrypt struct {
	xortbl []byte
}

// NewSimpleXORBlockCrypt initate BlockCrypt by the given key
func NewSimpleXORBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(simpleXORBlockCrypt)
	c.xortbl = pbkdf2.Key(key, []byte(saltxor), 32, mtuLimit, sha1.New)
	return c, nil
}

// Encrypt implements Encrypt interface
func (c *simpleXORBlockCrypt) Encrypt(dst, src []byte) { xorBytes(dst, src, c.xortbl) }

// Decrypt implements Decrypt interface
func (c *simpleXORBlockCrypt) Decrypt(dst, src []byte) { xorBytes(dst, src, c.xortbl) }

// noneBlockCrypt simple returns the plaintext
type noneBlockCrypt struct{}

// NewNoneBlockCrypt initate by the given key
func NewNoneBlockCrypt(key []byte) (BlockCrypt, error) {
	return new(noneBlockCrypt), nil
}

// Encrypt implements Encrypt interface
func (c *noneBlockCrypt) Encrypt(dst, src []byte) { copy(dst, src) }

// Decrypt implements Decrypt interface
func (c *noneBlockCrypt) Decrypt(dst, src []byte) { copy(dst, src) }

// packet encryption with local CFB mode
func encrypt(block cipher.Block, dst, src, buf []byte) {
	blocksize := block.BlockSize()
	tbl := buf[:blocksize]
	block.Encrypt(tbl, initialVector)
	n := len(src) / blocksize
	base := 0
	for i := 0; i < n; i++ {
		xorWords(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += blocksize
	}
	xorBytes(dst[base:], src[base:], tbl)
}

func decrypt(block cipher.Block, dst, src, buf []byte) {
	blocksize := block.BlockSize()
	tbl := buf[:blocksize]
	next := buf[blocksize:]
	block.Encrypt(tbl, initialVector)
	n := len(src) / blocksize
	base := 0
	for i := 0; i < n; i++ {
		block.Encrypt(next, src[base:])
		xorWords(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += blocksize
	}
	xorBytes(dst[base:], src[base:], tbl)
}
