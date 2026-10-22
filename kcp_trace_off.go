//go:build !debug

// if build tag debug is not set, debugLog is a no-op eliminated at compile time
package kcp

func (kcp *KCP) debugLog(logtype KCPLogType, args ...any) {}

// kcpTrace guards debugLog calls, so their arguments are not built when off.
const kcpTrace = false
