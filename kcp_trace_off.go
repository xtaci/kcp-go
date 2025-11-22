//go:build !debug

// if build tag debug is not set, then DebugLog will ingore in compile time
package kcp

func (kcp *KCP) DebugLog(logtype KCPLogType, args ...any) {}
