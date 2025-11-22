//go:build debug

// only build tag debug is set, then DebugLog will be enabled in compile time
package kcp

func (kcp *KCP) DebugLog(logtype KCPLogType, args ...any) {
	if kcp.logmask&logtype == 0 {
		return
	}

	var msg string
	switch logtype {
	case IKCP_LOG_OUTPUT:
		msg = "kcp output"
	case IKCP_LOG_INPUT:
		msg = "kcp input"
	case IKCP_LOG_SEND:
		msg = "kcp send"
	case IKCP_LOG_RECV:
		msg = "kcp recv"
	case IKCP_LOG_OUT_ACK:
		msg = "kcp output ack"
	case IKCP_LOG_OUT_PUSH:
		msg = "kcp output push"
	case IKCP_LOG_OUT_WASK:
		msg = "kcp output wask"
	case IKCP_LOG_OUT_WINS:
		msg = "kcp output wins"
	case IKCP_LOG_IN_ACK:
		msg = "kcp input ack"
	case IKCP_LOG_IN_PUSH:
		msg = "kcp input push"
	case IKCP_LOG_IN_WASK:
		msg = "kcp input wask"
	case IKCP_LOG_IN_WINS:
		msg = "kcp input wins"
	}
	kcp.logoutput(msg, args...)
}
