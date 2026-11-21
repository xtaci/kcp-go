// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix || darwin || netbsd

package socket

import (
	"net"
	"os"
	"sync"
	"syscall"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

type mmsghdrs []mmsghdr

func (hs mmsghdrs) unpack(ms []ipv4.Message, parseFn func([]byte, string) (net.Addr, error), hint string) error {
	for i := range hs {
		ms[i].N = int(hs[i].Len)
		ms[i].NN = msghdrControlLen(&hs[i].Hdr)
		ms[i].Flags = msghdrFlags(&hs[i].Hdr)
		if parseFn != nil {
			var err error
			ms[i].Addr, err = parseFn(msghdrName(&hs[i].Hdr), hint)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// mmsghdrsPacker packs Message-slices into mmsghdrs (re-)using pre-allocated buffers.
type mmsghdrsPacker struct {
	// hs are the pre-allocated mmsghdrs.
	hs mmsghdrs
	// sockaddrs is the pre-allocated buffer for the Hdr.Name buffers.
	// We use one large buffer for all messages and slice it up.
	sockaddrs []byte
	// vs are the pre-allocated iovecs.
	// We allocate one large buffer for all messages and slice it up. This allows to reuse the buffer
	// if the number of buffers per message is distributed differently between calls.
	vs []unix.Iovec
}

func (p *mmsghdrsPacker) prepare(ms []ipv4.Message) {
	n := len(ms)
	if n <= cap(p.hs) {
		p.hs = p.hs[:n]
	} else {
		p.hs = make(mmsghdrs, n)
	}
	if n*unix.SizeofSockaddrInet6 <= cap(p.sockaddrs) {
		p.sockaddrs = p.sockaddrs[:n*unix.SizeofSockaddrInet6]
	} else {
		p.sockaddrs = make([]byte, n*unix.SizeofSockaddrInet6)
	}

	nb := 0
	for _, m := range ms {
		nb += len(m.Buffers)
	}
	if nb <= cap(p.vs) {
		p.vs = p.vs[:nb]
	} else {
		p.vs = make([]unix.Iovec, nb)
	}
}

func (p *mmsghdrsPacker) pack(ms []ipv4.Message, parseFn func([]byte, string) (net.Addr, error), marshalFn func(net.Addr, []byte) int) mmsghdrs {
	p.prepare(ms)
	hs := p.hs
	vsRest := p.vs
	saRest := p.sockaddrs
	for i := range hs {
		nvs := len(ms[i].Buffers)
		vs := vsRest[:nvs]
		vsRest = vsRest[nvs:]

		var sa []byte
		switch {
		case parseFn != nil:
			sa = saRest[:unix.SizeofSockaddrInet6]
			saRest = saRest[unix.SizeofSockaddrInet6:]
		case marshalFn != nil:
			n := marshalFn(ms[i].Addr, saRest)
			if n > 0 {
				sa = saRest[:n]
				saRest = saRest[n:]
			}
		}
		msghdrPack(&hs[i].Hdr, vs, ms[i].Buffers, ms[i].OOB, sa)
	}
	return hs
}

// syscaller is a helper to invoke recvmsgs and sendmsgs via the RawConn.Read/Write interface.
// It is reusable, to amortize the overhead of allocating a closure for the function passed to
// RawConn.Read/Write.
type syscaller struct {
	n     int
	operr error
	hs    mmsghdrs
	flags int

	boundRecvmsgsF func(uintptr) bool
	boundSendmsgsF func(uintptr) bool
}

func (r *syscaller) init() {
	r.boundRecvmsgsF = r.recvmsgsF
	r.boundSendmsgsF = r.sendmsgsF
}

func (r *syscaller) recvmsgs(c syscall.RawConn, hs mmsghdrs, flags int) (int, error) {
	r.n = 0
	r.operr = nil
	r.hs = hs
	r.flags = flags
	if err := c.Read(r.boundRecvmsgsF); err != nil {
		return r.n, err
	}
	if r.operr != nil {
		return r.n, os.NewSyscallError("recvmsgs", r.operr)
	}
	return r.n, nil
}

func (r *syscaller) recvmsgsF(s uintptr) bool {
	r.n, r.operr = recvmsgs(s, r.hs, r.flags)
	return ioComplete(r.flags, r.operr)
}

func (r *syscaller) sendmsgs(c syscall.RawConn, hs mmsghdrs, flags int) (int, error) {
	r.n = 0
	r.operr = nil
	r.hs = hs
	r.flags = flags
	if err := c.Write(r.boundSendmsgsF); err != nil {
		return r.n, err
	}
	if r.operr != nil {
		return r.n, os.NewSyscallError("sendmsgs", r.operr)
	}
	return r.n, nil
}

func (r *syscaller) sendmsgsF(s uintptr) bool {
	r.n, r.operr = sendmsgs(s, r.hs, r.flags)
	return ioComplete(r.flags, r.operr)
}

// mmsgTmps holds reusable temporary helpers for recvmsgs and sendmsgs.
type mmsgTmps struct {
	packer    mmsghdrsPacker
	syscaller syscaller
}

var defaultMmsgTmpsPool = mmsgTmpsPool{
	p: sync.Pool{
		New: func() any {
			tmps := new(mmsgTmps)
			tmps.syscaller.init()
			return tmps
		},
	},
}

type mmsgTmpsPool struct {
	p sync.Pool
}

func (p *mmsgTmpsPool) Get() *mmsgTmps {
	m := p.p.Get().(*mmsgTmps)
	// Clear fields up to the len (not the cap) of the slice,
	// assuming that the previous caller only used that many elements.
	for i := range m.packer.sockaddrs {
		m.packer.sockaddrs[i] = 0
	}
	m.packer.sockaddrs = m.packer.sockaddrs[:0]
	for i := range m.packer.vs {
		m.packer.vs[i] = unix.Iovec{}
	}
	m.packer.vs = m.packer.vs[:0]
	for i := range m.packer.hs {
		m.packer.hs[i].Len = 0
		m.packer.hs[i].Hdr = unix.Msghdr{}
	}
	m.packer.hs = m.packer.hs[:0]
	return m
}

func (p *mmsgTmpsPool) Put(tmps *mmsgTmps) {
	p.p.Put(tmps)
}
