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

// NOTE: We don't use generic because typed connKey() can be inlined calls.

type connManager interface {
	Compute(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool)
	Range(func(conn *UDPSession))
	Size() int
	Close()
}

// connKeyDefault is a peer endpoint tuple: (remote IP, remote UDP port).
type connKeyDefault = netip.AddrPort

type connManagerImplDefault struct {
	connections *xsync.Map[connKeyDefault, *UDPSession]
}

// connKey returns the remote endpoint tuple (IP, UDP port).
func (*connManagerImplDefault) connKey(remoteAddr *net.UDPAddr) connKeyDefault {
	return remoteAddr.AddrPort()
}

// Compute atomically updates the session associated with the remote address.
func (manager *connManagerImplDefault) Compute(_ libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool) {
	key := manager.connKey(remoteAddr)
	return manager.connections.Compute(key, valueFn)
}

// Range visits the currently registered sessions without removing them.
func (manager *connManagerImplDefault) Range(fn func(conn *UDPSession)) {
	manager.connections.DeleteMatching(func(_ connKeyDefault, conn *UDPSession) (bool, bool) {
		fn(conn)
		return false, false
	})
}

// Size returns the number of registered sessions.
func (manager *connManagerImplDefault) Size() int {
	return manager.connections.Size()
}

// Close releases and removes all registered sessions.
func (manager *connManagerImplDefault) Close() {
	manager.connections.DeleteMatching(func(_ connKeyDefault, conn *UDPSession) (bool, bool) {
		conn.release()
		return true, false
	})
}

// newConnManager creates a manager that keys sessions by remote address only.
func newConnManager() connManager {
	return &connManagerImplDefault{
		connections: xsync.NewMap[connKeyDefault, *UDPSession](),
	}
}

// connKeyUnspecified is the tuple
// (local destination IP, ingress interface index, remote IP, remote UDP port).
// Including the local destination keeps sessions from one peer to different
// local addresses distinct on wildcard listeners.
type connKeyUnspecified struct {
	local  libnetip.AddrInterface
	remote netip.AddrPort
}

type connManagerImplUnspecified struct {
	connections *xsync.Map[connKeyUnspecified, *UDPSession]
}

// connKey returns the local-destination and remote-endpoint tuple.
func (*connManagerImplUnspecified) connKey(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr) connKeyUnspecified {
	return connKeyUnspecified{
		local:  laddr,
		remote: remoteAddr.AddrPort(),
	}
}

// Compute atomically updates the session for the local and remote addresses.
func (manager *connManagerImplUnspecified) Compute(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool) {
	key := manager.connKey(laddr, remoteAddr)
	return manager.connections.Compute(key, valueFn)
}

// Range visits the currently registered sessions without removing them.
func (manager *connManagerImplUnspecified) Range(fn func(conn *UDPSession)) {
	manager.connections.DeleteMatching(func(_ connKeyUnspecified, conn *UDPSession) (bool, bool) {
		fn(conn)
		return false, false
	})
}

// Size returns the number of registered sessions.
func (manager *connManagerImplUnspecified) Size() int {
	return manager.connections.Size()
}

// Close releases and removes all registered sessions.
func (manager *connManagerImplUnspecified) Close() {
	manager.connections.DeleteMatching(func(_ connKeyUnspecified, conn *UDPSession) (bool, bool) {
		conn.release()
		return true, false
	})
}

// newConnManagerUnspecified creates a manager for a wildcard listener.
func newConnManagerUnspecified() connManager {
	return &connManagerImplUnspecified{
		connections: xsync.NewMap[connKeyUnspecified, *UDPSession](),
	}
}

// connManagerImpl wraps the underlying implementation so callers can treat
// default and unspecified managers uniformly under a single type.
// It asserts that the underlying implementation matches a known concrete type
// (panicking otherwise), allowing the compiler to prove no heap escape occurs.
type connManagerImpl struct {
	connManager
}

func (manager *connManagerImpl) Compute(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool) {
	switch manager := manager.connManager.(type) {
	case *connManagerImplDefault:
		return manager.Compute(laddr, remoteAddr, valueFn)
	case *connManagerImplUnspecified:
		return manager.Compute(laddr, remoteAddr, valueFn)
	default:
		panic("unreachable")
	}
}

func (manager *connManagerImpl) Range(fn func(conn *UDPSession)) {
	switch manager := manager.connManager.(type) {
	case *connManagerImplDefault:
		manager.Range(fn)
	case *connManagerImplUnspecified:
		manager.Range(fn)
	default:
		panic("unreachable")
	}
}
