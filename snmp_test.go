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

//go:build !kcp_no_snmp

package kcp

import (
	"net"
	"sync/atomic"
	"testing"
)

func TestSNMP(t *testing.T) {
	snmp := newSnmp()
	snmp.BytesSent = 123
	snmp.FECRecovered = 45
	snmp.OOBPackets = 6

	header := snmp.Header()
	values := snmp.ToSlice()
	if len(header) != len(values) {
		t.Fatalf("Header() has %d values, ToSlice() has %d", len(header), len(values))
	}
	if header[0] != "BytesSent" || values[0] != "123" {
		t.Fatalf("BytesSent = %q:%q, want %q:%q", header[0], values[0], "BytesSent", "123")
	}

	snapshot := snmp.Copy()
	snmp.BytesSent = 999
	if snapshot.BytesSent != 123 {
		t.Fatalf("Copy().BytesSent = %d, want 123", snapshot.BytesSent)
	}

	snmp.Reset()
	if got := snmp.Copy(); *got != (Snmp{}) {
		t.Fatalf("Reset() = %+v, want zero Snmp", got)
	}
	for i, value := range snmp.ToSlice() {
		if value != "0" {
			t.Fatalf("ToSlice()[%d] = %q after Reset, want %q", i, value, "0")
		}
	}
}

func TestPacketPusherPinsFirstPeer(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	sess, err := NewConn3(1, nil, nil, 0, 0, &pushConn{UDPConn: udp})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	first := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	other := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}
	pkt := make([]byte, IKCP_OVERHEAD)
	before := atomic.LoadUint64(&DefaultSnmp.InErrs)
	sess.pushedPacket(pkt, first, nil)
	sess.pushedPacket(pkt, first, nil)
	if got := atomic.LoadUint64(&DefaultSnmp.InErrs) - before; got != 0 {
		t.Fatalf("packets from the first peer counted %d errors", got)
	}
	sess.pushedPacket(pkt, other, nil)
	if got := atomic.LoadUint64(&DefaultSnmp.InErrs) - before; got != 1 {
		t.Fatalf("a packet from another peer counted %d errors, want 1", got)
	}
}
