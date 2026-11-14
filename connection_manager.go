// The MIT License (MIT)
//
// Copyright (c) 2026 Coia Prant
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

package kcp

import (
	"net"
	"net/netip"

	"github.com/puzpuzpuz/xsync/v4"
	libnetip "github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
)

// NOTE: We don't use generic because typed connectionKey() can be inlined calls.

type connectionManager interface {
	Compute(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool)
	Range(func(conn *UDPSession))
	Size() int
	Close()
}

type connectionKey = netip.AddrPort

type connectionManagerImpl struct {
	connections *xsync.Map[connectionKey, *UDPSession]
}

func (*connectionManagerImpl) connectionKey(remoteAddr *net.UDPAddr) connectionKey {
	return remoteAddr.AddrPort()
}

func (manager *connectionManagerImpl) Compute(_ libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool) {
	key := manager.connectionKey(remoteAddr)
	return manager.connections.Compute(key, valueFn)
}

func (manager *connectionManagerImpl) Range(fn func(conn *UDPSession)) {
	manager.connections.DeleteMatching(func(_ connectionKey, conn *UDPSession) (bool, bool) {
		fn(conn)
		return false, false
	})
}

func (manager *connectionManagerImpl) Size() int {
	return manager.connections.Size()
}

func (manager *connectionManagerImpl) Close() {
	manager.connections.DeleteMatching(func(_ connectionKey, conn *UDPSession) (bool, bool) {
		conn.release()
		return true, false
	})
}

func newConnectionManager() connectionManager {
	return &connectionManagerImpl{
		connections: xsync.NewMap[connectionKey, *UDPSession](),
	}
}

type connectionKeyUnspecified struct {
	local  libnetip.AddrInterface
	remote netip.AddrPort
}

type connectionManagerImplUnspecified struct {
	connections *xsync.Map[connectionKeyUnspecified, *UDPSession]
}

func (*connectionManagerImplUnspecified) connectionKey(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr) connectionKeyUnspecified {
	return connectionKeyUnspecified{
		local:  laddr,
		remote: remoteAddr.AddrPort(),
	}
}

func (manager *connectionManagerImplUnspecified) Compute(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool) {
	key := manager.connectionKey(laddr, remoteAddr)
	return manager.connections.Compute(key, valueFn)
}

func (manager *connectionManagerImplUnspecified) Range(fn func(conn *UDPSession)) {
	manager.connections.DeleteMatching(func(_ connectionKeyUnspecified, conn *UDPSession) (bool, bool) {
		fn(conn)
		return false, false
	})
}

func (manager *connectionManagerImplUnspecified) Size() int {
	return manager.connections.Size()
}

func (manager *connectionManagerImplUnspecified) Close() {
	manager.connections.DeleteMatching(func(_ connectionKeyUnspecified, conn *UDPSession) (bool, bool) {
		conn.release()
		return true, false
	})
}

func newConnectionManagerUnspecified() connectionManager {
	return &connectionManagerImplUnspecified{
		connections: xsync.NewMap[connectionKeyUnspecified, *UDPSession](),
	}
}
