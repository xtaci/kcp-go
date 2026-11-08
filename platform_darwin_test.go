//go:build darwin

package kcp

import (
	"net"
	"testing"

	"golang.org/x/net/ipv4"
)

func TestDarwinBatchConn(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			receiver, err := net.ListenPacket(network, map[string]string{
				"udp4": "127.0.0.1:0",
				"udp6": "[::1]:0",
			}[network])
			if err != nil {
				if network == "udp6" {
					t.Skipf("IPv6 loopback unavailable: %v", err)
				}
				t.Fatal(err)
			}
			defer receiver.Close()

			sender, err := net.ListenPacket(network, map[string]string{
				"udp4": "127.0.0.1:0",
				"udp6": "[::1]:0",
			}[network])
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()

			writer := newBatchConn(sender)
			reader := newBatchConn(receiver)
			if writer == nil || reader == nil {
				t.Fatal("UDP sockets did not get Darwin batch connections")
			}

			want := [][]byte{[]byte("first packet"), []byte("second packet")}
			out := make([]ipv4.Message, len(want))
			for i := range want {
				out[i] = ipv4.Message{
					Buffers: [][]byte{want[i]},
					Addr:    receiver.LocalAddr(),
				}
			}
			n, err := writer.WriteBatch(out, 0)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(want) {
				t.Fatalf("WriteBatch wrote %d packets, want %d", n, len(want))
			}

			in := make([]ipv4.Message, len(want))
			for i := range in {
				in[i].Buffers = [][]byte{make([]byte, 64)}
			}
			n, err = reader.ReadBatch(in, 0)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(want) {
				t.Fatalf("ReadBatch read %d packets, want %d", n, len(want))
			}
			for i := range want {
				if got := string(in[i].Buffers[0][:in[i].N]); got != string(want[i]) {
					t.Errorf("packet %d = %q, want %q", i, got, want[i])
				}
				if in[i].Addr == nil {
					t.Errorf("packet %d has no source address", i)
				}
			}
		})
	}
}
