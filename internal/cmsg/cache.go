package cmsg

import (
	"net"
	"time"
	"weak"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/xtaci/kcp-go/v5/internal/lib/net/netip"
	"github.com/xtaci/kcp-go/v5/internal/lib/net/zone"
)

var (
	cacheUDPAddr        = xsync.NewMap[netip.AddrInterfacePort, weak.Pointer[net.UDPAddr]]()
	cacheControlMessage = xsync.NewMap[netip.AddrInterface, weak.Pointer[[]byte]]()
)

func init() {
	if !SupportsControlMessage {
		return
	}

	go cacheCleanup()
}

func cacheCleanup() {
	for range time.Tick(time.Hour) {
		cacheUDPAddr.DeleteMatching(func(_ netip.AddrInterfacePort, v weak.Pointer[net.UDPAddr]) (bool, bool) {
			return v.Value() == nil, false
		})
		cacheControlMessage.DeleteMatching(func(_ netip.AddrInterface, v weak.Pointer[[]byte]) (bool, bool) {
			return v.Value() == nil, false
		})
	}
}

func ResolveUDPAddr(laddr netip.AddrInterface, port uint16) (ret *net.UDPAddr) {
	key := netip.AddrInterfacePortFrom(laddr, port)
	cacheUDPAddr.Compute(key, func(v weak.Pointer[net.UDPAddr], _ bool) (weak.Pointer[net.UDPAddr], xsync.ComputeOp) {
		if ret = v.Value(); ret != nil {
			return v, xsync.CancelOp
		}

		ret = &net.UDPAddr{
			IP:   net.IP(laddr.Addr().AsSlice()),
			Port: int(port),
		}
		if laddr.Addr().Is6() && laddr.Addr().IsLinkLocalUnicast() {
			ret.Zone = zone.Name(int(laddr.IfIndex()))
		}

		return weak.Make(ret), xsync.UpdateOp
	})
	return
}

func AcquireControlMessage(laddr netip.AddrInterface) (ret *[]byte) {
	cacheControlMessage.Compute(laddr, func(v weak.Pointer[[]byte], _ bool) (weak.Pointer[[]byte], xsync.ComputeOp) {
		if ret = v.Value(); ret != nil {
			return v, xsync.CancelOp
		}

		ret = buildControlMessage(laddr)
		return weak.Make(ret), xsync.UpdateOp
	})
	return
}
