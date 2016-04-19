# kcp-go
[![GoDoc][1]][2] [![Build Status][3]][4] [![Go Report Card][5]][6]
[1]: https://godoc.org/github.com/xtaci/kcp-go?status.svg
[2]: https://godoc.org/github.com/xtaci/kcp-go
[3]: https://travis-ci.org/xtaci/kcp-go.svg?branch=master
[4]: https://travis-ci.org/xtaci/kcp-go
[5]: https://goreportcard.com/badge/xtaci/kcp-go
[6]: https://goreportcard.com/report/xtaci/kcp-go
Coverage: http://gocover.io/github.com/xtaci/kcp-go            

A port of [KCP](https://github.com/skywind3000/kcp) by [skywind3000](https://github.com/skywind3000) in [golang](https://golang.org/)

# Features -- 特性
1. 100% compatible with original C version.     -- 100%兼容ikcp.c，紧跟上游
2. Pure golang implementation of KCP in a single file [kcp.go](https://github.com/xtaci/kcp-go/blob/master/kcp.go).  --  纯golang单文件实现，能与上游保持完全兼容，也易于提出来独立使用
2. Instead of container.List, kcp-go made use of slice based internal queue.   -- slice优化的传输队列 
3. Provides a basic [session manager](https://github.com/xtaci/kcp-go/blob/master/sess.go), compatible with [net.Conn](https://golang.org/pkg/net/#Conn) and [net.Listener](https://golang.org/pkg/net/#Listener).  -- 接口兼容net.Conn/net.Listener
4. Seperated KCP code and session manager code, you can use kcp.go only without session manager.  -- 独立的会话管理，不影响kcp核心
5. Highly secure packet level encryption(AES-256-CFB-MD5-OTP) without signature. -- 高安全的数据包加密，保障公民信息安全

# Encryption Process Diagram -- 加密流程图
```
                rand.Reader
                     +
                     |
                     |                 +---------+   PSK   +-----------+
                  +--v--+              |                               |
                  | OTP |              |                               |
                  +--+--+              |                               |
                     |                 |                               |
+--------+       +---v----+       +----+----+                     +----+----+        +--------+
|        |       |        |       |         |                     |         |        |        |
|  DATA  +-------> PACKET +-------->AES+CFB +----> Internet +------>AES+CFB +--------> PACKET |
|        |       |        |       | ENCRYPT |                     | DECRYPT |        |        |
+---+----+       +---^----+       |         |                     |         |        +---+----+
    |                |            +---------+                     +---------+            |
    |           +----+----+                                                              |
    |           |         |                                                          +---v----+       +--------+
    +----------->   MD5   |                                                          |        |       |        |
                |         |                                                          |  MD5   +------->  DATA  |
                +---------+                                                          | VERIFY |       |        |
                                                                                     |        |       +--------+
                                                                                     +--------+
```

# Conventions  -- 实现约定
1. use UDP for packet delivery.   -- 使用UDP传输数据
2. ```conv uint32``` in session manager is a random number initiated by client.   -- conv由客户端产生随机数，用于区分会话
3. conn.Write never blocks in KCP, so conn.SetWriteDeadline has no use.  -- 写无阻塞
4. KCP doesn't define control messages like SYN/FIN/RST in TCP, a real world example is to use TCP & KCP at the same time, of which TCP does session control(eg. UDP disconnecting.), and UDP does message delivery.   -- KCP本身不提供链路控制，需要结合TCP实现链路开合控制

# Performance  -- 性能
```
$ go test -run TestSpeed
new client 127.0.0.1:57296
total recv: 1048576
time for 1MB rtt 52.313861ms
PASS
ok  	_/Users/xtaci/kcp-go	0.063s
```

# Related Applications  -- 相关应用
1. [kcptun](https://github.com/xtaci/kcptun) -- TCP流转换为KCP+UDP流

# Contribution  -- 关于贡献
PR is welcome if the code is short and clean.  -- 欢迎简洁的PR
