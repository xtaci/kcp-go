package kcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"crypto/sha1"
	"golang.org/x/crypto/chacha20poly1305"
	"io"
	"strconv"
	"sync"

	"github.com/templexxx/xor"
	"github.com/tjfoc/gmsm/sm4"

	"errors"
	"golang.org/x/crypto/blowfish"
	"golang.org/x/crypto/cast5"
	"golang.org/x/crypto/hkdf"
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

type KeySizeError int

func (e KeySizeError) Error() string {
	return "key size error: need " + strconv.Itoa(int(e)) + " bytes"
}

// BlockCrypt defines encryption/decryption methods for a given byte slice.
// Notes on implementing: the data to be encrypted contains a builtin
// nonce at the first 16 bytes
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

// NewSalsa20BlockCrypt https://en.wikipedia.org/wiki/Salsa20
func NewSalsa20BlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(salsa20BlockCrypt)
	copy(c.key[:], key)
	return c, nil
}

func (c *salsa20BlockCrypt) Encrypt(dst, src []byte) {
	salsa20.XORKeyStream(dst[8:], src[8:], src[:8], &c.key)
	copy(dst[:8], src[:8])
}
func (c *salsa20BlockCrypt) Decrypt(dst, src []byte) {
	salsa20.XORKeyStream(dst[8:], src[8:], src[:8], &c.key)
	copy(dst[:8], src[:8])
}

type sm4BlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewSM4BlockCrypt https://github.com/tjfoc/gmsm/tree/master/sm4
func NewSM4BlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(sm4BlockCrypt)
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	c.encbuf = make([]byte, sm4.BlockSize)
	c.decbuf = make([]byte, 2*sm4.BlockSize)
	return c, nil
}

func (c *sm4BlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *sm4BlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type twofishBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewTwofishBlockCrypt https://en.wikipedia.org/wiki/Twofish
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

func (c *twofishBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *twofishBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type tripleDESBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewTripleDESBlockCrypt https://en.wikipedia.org/wiki/Triple_DES
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

func (c *tripleDESBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *tripleDESBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type cast5BlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewCast5BlockCrypt https://en.wikipedia.org/wiki/CAST-128
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

func (c *cast5BlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *cast5BlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type blowfishBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewBlowfishBlockCrypt https://en.wikipedia.org/wiki/Blowfish_(cipher)
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

func (c *blowfishBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *blowfishBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type aesBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewAESBlockCrypt https://en.wikipedia.org/wiki/Advanced_Encryption_Standard
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

func (c *aesBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *aesBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type teaBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewTEABlockCrypt https://en.wikipedia.org/wiki/Tiny_Encryption_Algorithm
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

func (c *teaBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *teaBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type xteaBlockCrypt struct {
	encbuf []byte
	decbuf []byte
	block  cipher.Block
}

// NewXTEABlockCrypt https://en.wikipedia.org/wiki/XTEA
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

func (c *xteaBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf) }
func (c *xteaBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf) }

type simpleXORBlockCrypt struct {
	xortbl []byte
}

// NewSimpleXORBlockCrypt simple xor with key expanding
func NewSimpleXORBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(simpleXORBlockCrypt)
	c.xortbl = pbkdf2.Key(key, []byte(saltxor), 32, mtuLimit, sha1.New)
	return c, nil
}

func (c *simpleXORBlockCrypt) Encrypt(dst, src []byte) { xor.Bytes(dst, src, c.xortbl) }
func (c *simpleXORBlockCrypt) Decrypt(dst, src []byte) { xor.Bytes(dst, src, c.xortbl) }

type noneBlockCrypt struct{}

// NewNoneBlockCrypt does nothing but copying
func NewNoneBlockCrypt(key []byte) (BlockCrypt, error) {
	return new(noneBlockCrypt), nil
}

func (c *noneBlockCrypt) Encrypt(dst, src []byte) { copy(dst, src) }
func (c *noneBlockCrypt) Decrypt(dst, src []byte) { copy(dst, src) }

// packet encryption with local CFB mode
func encrypt(block cipher.Block, dst, src, buf []byte) {
	blocksize := block.BlockSize()
	tbl := buf[:blocksize]
	block.Encrypt(tbl, initialVector)
	n := len(src) / blocksize
	base := 0
	for i := 0; i < n; i++ {
		xor.BytesSrc1(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += blocksize
	}
	xor.BytesSrc0(dst[base:], src[base:], tbl)
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
		xor.BytesSrc1(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += blocksize
	}
	xor.BytesSrc0(dst[base:], src[base:], tbl)
}

// Add ahead encryption & decryption
var ErrShortPacket = errors.New("short packet")
var _zerononce [128]byte

type AheadCipher interface {
	KeySize() int
	SaltSize() int

	Encrypt(dst, src []byte) ([]byte, error)
	Decrypt(dst, pkt []byte) ([]byte, error)

	Encrypter(salt []byte) (cipher.AEAD, error)
	Decrypter(salt []byte) (cipher.AEAD, error)
}

type metaAheadBlockCipher struct{
	psk	[]byte
	makeAEAD func(key []byte) (cipher.AEAD, error)

	Buf []byte
	sync.Mutex
}

func (c *metaAheadBlockCipher)KeySize() int{
	return len(c.psk)
}
func (c *metaAheadBlockCipher)SaltSize() int{
	if ks := c.KeySize(); ks > 16{
		return ks
	}
	return 16
}
func hkdfSHA1(secret, salt, info, outkey []byte) {
	r := hkdf.New(sha1.New, secret, salt, info)
	if _, err := io.ReadFull(r, outkey); err != nil {
		panic(err) // should never happen
	}
}

func (c *metaAheadBlockCipher) Encrypter(salt []byte) (cipher.AEAD, error) {
	subkey := make([]byte, c.KeySize())
	hkdfSHA1(c.psk, salt, []byte("too simple, sometimes naive"), subkey)
	return c.makeAEAD(subkey)
}
func (c *metaAheadBlockCipher) Decrypter(salt []byte) (cipher.AEAD, error) {
	subkey := make([]byte, c.KeySize())
	hkdfSHA1(c.psk, salt, []byte("too simple, sometimes naive"), subkey)
	return c.makeAEAD(subkey)
}


func (c *metaAheadBlockCipher)Encrypt(dst, src []byte) ([]byte, error) {
	saltSize := c.SaltSize()
	salt := dst[:saltSize]
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	aead, err := c.Encrypter(salt)
	if err != nil {
		return nil, err
	}

	if len(dst) < saltSize+len(src)+aead.Overhead() {
		return nil, io.ErrShortBuffer
	}
	b := aead.Seal(dst[saltSize:saltSize], _zerononce[:aead.NonceSize()], src, nil)
	return dst[:saltSize+len(b)], nil
}
func (c *metaAheadBlockCipher)Decrypt(dst, pkt []byte) ([]byte, error) {
	saltSize := c.SaltSize()
	if len(pkt) < saltSize {
		return nil, ErrShortPacket
	}
	salt := pkt[:saltSize]
	aead, err := c.Decrypter(salt)
	if err != nil {
		return nil, err
	}
	if len(pkt) < saltSize+aead.Overhead() {
		return nil, ErrShortPacket
	}
	if saltSize+len(dst)+aead.Overhead() < len(pkt) {
		return nil, io.ErrShortBuffer
	}
	b, err := aead.Open(dst[:0], _zerononce[:aead.NonceSize()], pkt[saltSize:], nil)
	return b, err
}

func aesGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

func NewChacha20Ploy1305(key[] byte) (AheadCipher, error){
	if len(key) != chacha20poly1305.KeySize {
		return nil, KeySizeError(chacha20poly1305.KeySize)
	}
	return &metaAheadBlockCipher{psk: key, makeAEAD: chacha20poly1305.New, Buf: make([]byte, 2 * mtuLimit)}, nil
}

func NewAES128GCM(key[] byte) (AheadCipher, error){
	if len(key) != 16{
		return nil, KeySizeError(16)
	}
	return &metaAheadBlockCipher{psk: key, makeAEAD: aesGCM, Buf: make([]byte, 2 * mtuLimit)}, nil
}

func NewAES196GCM(key[] byte) (AheadCipher, error){
	if len(key) != 24{
		return nil, KeySizeError(24)
	}
	return &metaAheadBlockCipher{psk: key, makeAEAD: aesGCM, Buf: make([]byte, 2 * mtuLimit)}, nil
}

func NewAES256GCM(key[] byte) (AheadCipher, error){
	if len(key) != 32{
		return nil, KeySizeError(32)
	}
	return &metaAheadBlockCipher{psk: key, makeAEAD: aesGCM, Buf: make([]byte, 2 * mtuLimit)}, nil
}