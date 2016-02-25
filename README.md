# kcp-go
[![GoDoc][1]][2]
[1]: https://godoc.org/github.com/xtaci/kcp-go?status.svg
[2]: https://godoc.org/github.com/xtaci/kcp-go
A port of [KCP](https://github.com/skywind3000/kcp) in [golang](https://golang.org/)

# Status
Alpha

# Features
1. Pure golang implementation of KCP in a single file kcp.go.
2. Instead of container.List, kcp-go use slice based internal queue. 
3. Provide a basic session manager, compatible with net.Conn and net.Listener.
4. Sepearted KCP code and session manager code, you can use kcp.go only without session manager.

# Conventions
1. ```conv uint32``` in session manager is a random number initiated by client
2. conn.Write never blocks in KCP, so conn.SetWriteDeadline has no use.
