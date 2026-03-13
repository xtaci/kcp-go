//go:build !debug

// if build tag debug is not set, debugLog is a no-op eliminated at compile time
package kcp

func (kcp *KCP) debugLog(logtype KCPLogType, args ...any) {}
