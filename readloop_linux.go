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

//go:build linux

package kcp

import (
	"net"

	"github.com/pkg/errors"
	"github.com/xtaci/kcp-go/v5/internal/cmsg"
	libnetip "github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
	"golang.org/x/net/ipv4"
)

const (
	batchSize = 256 // max packets per recvmmsg/sendmmsg call
)

// readLoop is the optimized Linux read loop, using the recvmmsg syscall to
// receive multiple UDP packets in a single system call.
func (s *UDPSession) readLoop() {
	if s.platform.batchConn == nil {
		s.readLoopGeneric()
		return
	}

	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, maxPacketSize)}
	}

	for {
		count, err := s.platform.batchConn.ReadBatch(msgs, 0)
		if err != nil {
			s.notifyReadError(errors.WithStack(err))
			return
		}

		if s.isClosed() {
			return
		}

		for i := range count {
			msg := &msgs[i]

			remoteAddr, ok := msg.Addr.(*net.UDPAddr)
			if !ok {
				continue
			}

			// Ensure the packet comes from the same source.
			if !isSameUDPAddr(remoteAddr, s.remoteAddr) {
				DefaultSnmp.increaseInErrs()
				continue
			}

			// The source and packet size have been validated.
			s.dispatch(msg.Buffers[0][:msg.N])
		}
	}
}

// readLoop is the optimized Linux read loop, using the recvmmsg syscall to
// receive multiple UDP packets in a single system call.
func (l *Listener) readLoop() {
	batchConn := newBatchConn(l.conn)

	if batchConn == nil {
		l.readLoopGeneric()
		return
	}

	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, maxPacketSize)}
	}

	for {
		count, err := batchConn.ReadBatch(msgs, 0)
		if err != nil {
			l.notifyReadError(errors.WithStack(err))
			return
		}

		for i := range count {
			msg := &msgs[i]

			remoteAddr, ok := msg.Addr.(*net.UDPAddr)
			if !ok {
				continue
			}

			l.dispatch(msg.Buffers[0][:msg.N], libnetip.AddrInterface{}, nil, remoteAddr)
		}
	}
}

// readLoopUnspecified reads packet destinations in batches using recvmmsg.
func (l *Listener) readLoopUnspecified() {
	batchConn := newBatchConn(l.conn)
	if batchConn == nil {
		l.readLoopUnspecifiedGeneric()
		return
	}

	msgs := make([]ipv4.Message, batchSize)
	for k := range msgs {
		msgs[k].Buffers = [][]byte{make([]byte, maxPacketSize)}
		msgs[k].OOB = make([]byte, cmsgPacketSize)
	}

	for {
		count, err := batchConn.ReadBatch(msgs, 0)
		if err != nil {
			l.notifyReadError(errors.WithStack(err))
			return
		}

		for i := range count {
			msg := &msgs[i]

			remoteAddr, ok := msg.Addr.(*net.UDPAddr)
			if !ok {
				continue
			}

			laddr, err := cmsg.ParseControlMessage(msg.OOB[:msg.NN])
			if err != nil {
				continue
			}

			if !isUnicast(laddr.Addr()) {
				continue
			}

			localAddr := cmsg.ResolveUDPAddr(laddr, l.udpPort)
			l.dispatch(msg.Buffers[0][:msg.N], laddr, localAddr, remoteAddr)
		}
	}
}
