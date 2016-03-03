# kcp-go
[![GoDoc][1]][2] [![Build Status][3]][4]
[1]: https://godoc.org/github.com/xtaci/kcp-go?status.svg
[2]: https://godoc.org/github.com/xtaci/kcp-go
[3]: https://travis-ci.org/xtaci/kcp-go.svg?branch=master
[4]: https://travis-ci.org/xtaci/kcp-go
A port of [KCP](https://github.com/skywind3000/kcp) by [skywind3000](https://github.com/skywind3000) in [golang](https://golang.org/)

# Status
Beta

# Features
1. 100% compatible with original C version.
2. Pure golang implementation of KCP in a single file [kcp.go](https://github.com/xtaci/kcp-go/blob/master/kcp.go).
2. Instead of container.List, kcp-go made use of slice based internal queue. 
3. Provides a basic [session manager](https://github.com/xtaci/kcp-go/blob/master/sess.go), compatible with [net.Conn](https://golang.org/pkg/net/#Conn) and [net.Listener](https://golang.org/pkg/net/#Listener).
4. Seperated KCP code and session manager code, you can use kcp.go only without session manager.

# Conventions
1. ```conv uint32``` in session manager is a random number initiated by client
2. conn.Write never blocks in KCP, so conn.SetWriteDeadline has no use.

# Contribution
PR is welcome if the code is short and clean.
