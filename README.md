# kcp-go
[![GoDoc][1]][2] [![Build Status][3]][4]
[1]: https://godoc.org/github.com/xtaci/kcp-go?status.svg
[2]: https://godoc.org/github.com/xtaci/kcp-go
[3]: https://travis-ci.org/xtaci/kcp-go.svg?branch=master
[4]: https://travis-ci.org/xtaci/kcp-go
A port of [KCP](https://github.com/skywind3000/kcp) by [skywind3000](https://github.com/skywind3000) in [golang](https://golang.org/)

# Status(版本状态)
Beta

# Features(特性)
1. 100% compatible with original C version.(100%兼容ikcp.c)
2. Pure golang implementation of KCP in a single file [kcp.go](https://github.com/xtaci/kcp-go/blob/master/kcp.go).(纯golang实现)
2. Instead of container.List, kcp-go made use of slice based internal queue. (slice优化的传输队列)
3. Provides a basic [session manager](https://github.com/xtaci/kcp-go/blob/master/sess.go), compatible with [net.Conn](https://golang.org/pkg/net/#Conn) and [net.Listener](https://golang.org/pkg/net/#Listener).(接口兼容net.Conn/net.Listener)
4. Seperated KCP code and session manager code, you can use kcp.go only without session manager.(独立的会话管理)

# Conventions(实现约定)
1. ```conv uint32``` in session manager is a random number initiated by client. (conv由客户端产生随机数)
2. conn.Write never blocks in KCP, so conn.SetWriteDeadline has no use.(写无阻塞)
3. KCP doesn't define control messages like SYN/FIN/RST in TCP, a real world example is to use TCP & KCP at the same time, of which TCP does session control(eg. UDP disconnecting.), and UDP does message delivery. (需要结合TCP实现链路控制)

# Related Applications(相关应用)
1. [kcptun](https://github.com/xtaci/kcptun) -- TCP流转换为KCP+UDP流

# Contribution(关于贡献)
PR is welcome if the code is short and clean.(欢迎简洁的PR)
