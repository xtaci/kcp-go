//go:build aix || darwin || netbsd

package socket

import (
	"net"

	"golang.org/x/net/ipv4"
)

func (c *Conn) recvMsgs(ms []ipv4.Message, flags int) (int, error) {
	tmps := defaultMmsgTmpsPool.Get()
	defer defaultMmsgTmpsPool.Put(tmps)
	var parseFn func([]byte, string) (net.Addr, error)
	if c.network != "tcp" {
		parseFn = parseInetAddr
	}
	hs := tmps.packer.pack(ms, parseFn, nil)
	n, err := tmps.syscaller.recvmsgs(c.c, hs, flags)
	if err != nil {
		return n, err
	}
	if err := hs[:n].unpack(ms[:n], parseFn, c.network); err != nil {
		return n, err
	}
	return n, nil
}

func (c *Conn) sendMsgs(ms []ipv4.Message, flags int) (int, error) {
	tmps := defaultMmsgTmpsPool.Get()
	defer defaultMmsgTmpsPool.Put(tmps)
	var marshalFn func(net.Addr, []byte) int
	if c.network != "tcp" {
		marshalFn = marshalInetAddr
	}
	hs := tmps.packer.pack(ms, nil, marshalFn)
	n, err := tmps.syscaller.sendmsgs(c.c, hs, flags)
	if err != nil {
		return n, err
	}
	if err := hs[:n].unpack(ms[:n], nil, ""); err != nil {
		return n, err
	}
	return n, nil
}
