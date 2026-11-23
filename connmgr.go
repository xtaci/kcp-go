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

// connManager stores active Listener sessions keyed either by remote endpoint
// or, for wildcard listeners, by (local destination, remote endpoint).
type connManager struct {
	connectionsUnspecified *xsync.Map[connKeyUnspecified, *UDPSession]
	connections            *xsync.Map[connKey, *UDPSession]
}

// connKey is a peer endpoint tuple: (remote IP, remote UDP port).
type connKey = netip.AddrPort

// connKeyUnspecified is the tuple
// (local destination IP, ingress interface index, remote IP, remote UDP port).
// Including the local destination keeps sessions from one peer to different
// local addresses distinct on wildcard listeners.
type connKeyUnspecified struct {
	local  libnetip.AddrInterface
	remote netip.AddrPort
}

func (*connManager) key(remoteAddr *net.UDPAddr) connKey {
	return remoteAddr.AddrPort()
}

func (*connManager) keyUnspecified(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr) connKeyUnspecified {
	return connKeyUnspecified{
		local:  laddr,
		remote: remoteAddr.AddrPort(),
	}
}

// Load returns the session associated with the local and remote addresses, if present.
func (manager *connManager) Load(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr) (*UDPSession, bool) {
	if manager.connectionsUnspecified != nil {
		return manager.connectionsUnspecified.Load(manager.keyUnspecified(laddr, remoteAddr))
	}
	return manager.connections.Load(manager.key(remoteAddr))
}

// Compute atomically updates the session associated with the local and remote addresses.
func (manager *connManager) Compute(laddr libnetip.AddrInterface, remoteAddr *net.UDPAddr, valueFn func(oldValue *UDPSession, loaded bool) (*UDPSession, xsync.ComputeOp)) (*UDPSession, bool) {
	if manager.connectionsUnspecified != nil {
		return manager.connectionsUnspecified.Compute(manager.keyUnspecified(laddr, remoteAddr), valueFn)
	}
	return manager.connections.Compute(manager.key(remoteAddr), valueFn)
}

// Range visits the currently registered sessions without removing them.
func (manager *connManager) Range(fn func(conn *UDPSession)) {
	if manager.connectionsUnspecified != nil {
		manager.connectionsUnspecified.DeleteMatching(func(_ connKeyUnspecified, conn *UDPSession) (bool, bool) {
			fn(conn)
			return false, false
		})
		return
	}
	if manager.connections != nil {
		manager.connections.DeleteMatching(func(_ connKey, conn *UDPSession) (bool, bool) {
			fn(conn)
			return false, false
		})
	}
}

// Size returns the number of registered sessions.
func (manager *connManager) Size() int {
	if manager.connectionsUnspecified != nil {
		return manager.connectionsUnspecified.Size()
	}
	if manager.connections != nil {
		return manager.connections.Size()
	}
	return 0
}

// Close releases and removes all registered sessions.
func (manager *connManager) Close() {
	if manager.connectionsUnspecified != nil {
		manager.connectionsUnspecified.DeleteMatching(func(_ connKeyUnspecified, conn *UDPSession) (bool, bool) {
			conn.release()
			return true, false
		})
		return
	}
	if manager.connections != nil {
		manager.connections.DeleteMatching(func(_ connKey, conn *UDPSession) (bool, bool) {
			conn.release()
			return true, false
		})
	}
}

// newConnManager creates a manager that keys sessions by remote address only.
func newConnManager() connManager {
	return connManager{
		connections: xsync.NewMap[connKey, *UDPSession](),
	}
}

// newConnManagerUnspecified creates a manager for a wildcard listener.
func newConnManagerUnspecified() connManager {
	return connManager{
		connectionsUnspecified: xsync.NewMap[connKeyUnspecified, *UDPSession](),
	}
}
