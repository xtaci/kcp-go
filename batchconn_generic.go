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
	if detector, ok := xconn.(batchErrDetector); ok {
		return detector.ReadBatchUnavailable(err)
	}
	return false
}

func writeBatchUnavailable(xconn batchConn, err error) bool {
	if detector, ok := xconn.(batchErrDetector); ok {
		return detector.WriteBatchUnavailable(err)
	}
	return false
}
