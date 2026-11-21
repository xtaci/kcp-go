// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix || darwin || dragonfly || freebsd || netbsd || openbsd

package socket

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func msghdrPack(h *unix.Msghdr, vs []unix.Iovec, bs [][]byte, oob []byte, sa []byte) {
	for i := range vs {
		vs[i].Base = (*byte)(unsafe.Pointer(&bs[i][0]))
		vs[i].SetLen(len(bs[i]))
	}
	if len(vs) > 0 {
		h.Iov = &vs[0]
		h.SetIovlen(len(vs))
	}
	if len(oob) > 0 {
		h.Control = (*byte)(unsafe.Pointer(&oob[0]))
		h.SetControllen(len(oob))
	}
	if sa != nil {
		h.Name = (*byte)(unsafe.Pointer(&sa[0]))
		h.Namelen = uint32(len(sa))
	}
}

func msghdrName(h *unix.Msghdr) []byte {
	if h.Name != nil && h.Namelen > 0 {
		return (*[unix.SizeofSockaddrInet6]byte)(unsafe.Pointer(h.Name))[:h.Namelen]
	}
	return nil
}

func msghdrControlLen(h *unix.Msghdr) int {
	return int(h.Controllen)
}

func msghdrFlags(h *unix.Msghdr) int {
	return int(h.Flags)
}
