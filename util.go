package kcp

import (
	"io"
	"reflect"
	"unsafe"
)

func string2Bytes(s string) []byte {
	stringHeader := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{
		Data: stringHeader.Data,
		Len:  stringHeader.Len,
		Cap:  stringHeader.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bh))
}

func bytes2String(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func encode8u(p []byte, c byte) []byte {
	return ikcp_encode8u(p, c)
}

/* decode 8 bits unsigned int */
func decode8u(p []byte, c *byte) ([]byte, error) {
	if len(p) < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	return ikcp_decode8u(p, c), nil
}

/* encode 16 bits unsigned int (lsb) */
func encode16u(p []byte, w uint16) []byte {
	return ikcp_encode16u(p, w)
}

/* decode 16 bits unsigned int (lsb) */
func decode16u(p []byte, w *uint16) ([]byte, error) {
	if len(p) < 2 {
		return nil, io.ErrUnexpectedEOF
	}
	return ikcp_decode16u(p, w), nil
}

/* encode 32 bits unsigned int (lsb) */
func encode32u(p []byte, l uint32) []byte {
	return ikcp_encode32u(p, l)
}

/* decode 32 bits unsigned int (lsb) */
func decode32u(p []byte, l *uint32) ([]byte, error) {
	if len(p) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	return ikcp_decode32u(p, l), nil
}

/* encode 8 bits unsigned int */
func encode8uString(p []byte, str string) []byte {
	p = encode8u(p, byte(len(str)))
	copy(p, string2Bytes(str))
	return p[len(str):]
}

/* decode 8 bits unsigned int */
func decode8uString(p []byte, str *string) ([]byte, error) {
	var strlen byte
	var err error
	p, err = decode8u(p, &strlen)
	if err != nil {
		return nil, err
	}
	if len(p) < int(strlen) {
		return nil, io.ErrUnexpectedEOF
	}
	*str = bytes2String(p[:int(strlen)])
	return p[int(strlen):], nil
}
