// +build !linux

package kcp

import (
	"net"
)

func toBatchConn(c net.PacketConn) batchConn {
	if xconn, ok := c.(batchConn); ok {
		return xconn
	}
	return nil
}

func readBatchUnavailable(xconn batchConn, err error) bool {
	ret := false
	if detector, ok := xconn.(batchErrDetector); ok {
		ret = detector.ReadBatchUnavailable(err)
	}
	return ret
}

func writeBatchUnavailable(xconn batchConn, err error) bool {
	ret := false
	if detector, ok := xconn.(batchErrDetector); ok {
		ret = detector.WriteBatchUnavailable(err)
	}
	return ret
}
