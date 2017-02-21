<img src="kcp-go.png" alt="kcp-go" height="50px" />


[![GoDoc][1]][2] [![Powered][9]][10] [![MIT licensed][11]][12] [![Build Status][3]][4] [![Go Report Card][5]][6] [![Coverage Statusd][7]][8]

[1]: https://godoc.org/github.com/xtaci/kcp-go?status.svg
[2]: https://godoc.org/github.com/xtaci/kcp-go
[3]: https://travis-ci.org/xtaci/kcp-go.svg?branch=master
[4]: https://travis-ci.org/xtaci/kcp-go
[5]: https://goreportcard.com/badge/github.com/xtaci/kcp-go
[6]: https://goreportcard.com/report/github.com/xtaci/kcp-go
[7]: https://codecov.io/gh/xtaci/kcp-go/branch/master/graph/badge.svg
[8]: https://codecov.io/gh/xtaci/kcp-go
[9]: https://img.shields.io/badge/KCP-Powered-blue.svg
[10]: https://github.com/skywind3000/kcp
[11]: https://img.shields.io/badge/license-MIT-blue.svg
[12]: LICENSE

[![Claude_Shannon](shannon.jpg)](https://en.wikipedia.org/wiki/Claude_Shannon)

## Introduction

kcp-go is a full-featured ***Reliable-UDP*** library for golang. It provides ***reliable, ordered, and error-checked*** delivery of a stream of octets between applications running on hosts communicating over an IP network.

## Features

1. Optimized for ***Online Games, Audio/Video Streaming***.
1. Compatible with [skywind3000's](https://github.com/skywind3000) C version with optimizations.
1. ***Cache friendly*** and ***Memory optimized*** design in golang.
1. Compatible with [net.Conn](https://golang.org/pkg/net/#Conn) and [net.Listener](https://golang.org/pkg/net/#Listener).
1. [FEC(Forward Error Correction)](https://en.wikipedia.org/wiki/Forward_error_correction) Support with [Reed-Solomon Codes](https://en.wikipedia.org/wiki/Reed%E2%80%93Solomon_error_correction)
1. Packet level encryption support with [AES](https://en.wikipedia.org/wiki/Advanced_Encryption_Standard), [TEA](https://en.wikipedia.org/wiki/Tiny_Encryption_Algorithm), [3DES](https://en.wikipedia.org/wiki/Triple_DES), [Blowfish](https://en.wikipedia.org/wiki/Blowfish_(cipher)), [Cast5](https://en.wikipedia.org/wiki/CAST-128), [Salsa20]( https://en.wikipedia.org/wiki/Salsa20), etc. in [CFB](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation#Cipher_Feedback_.28CFB.29) mode.

## Conventions

Control messages like SYN/FIN/RST in TCP ***are not defined*** in KCP, you need some ***keepalive mechanims*** in the application-level. a real world example is to use some ***multiplexing*** protocol over session, such as [smux](https://github.com/xtaci/smux), see [kcptun](https://github.com/xtaci/kcptun) for example.

## Documentation

For complete documentation, see the associated [Godoc](https://godoc.org/github.com/xtaci/kcp-go).

## Specification

<img src="frame.png" alt="Frame Format" height="109px" />

## Usage

Client:   [full demo](https://github.com/xtaci/kcptun/blob/master/client/main.go#L231)
```go
kcpconn, err := kcp.DialWithOptions("192.168.0.1:10000", nil, 10, 3)
```
Server:   [full demo](https://github.com/xtaci/kcptun/blob/master/server/main.go#L235)
```go
lis, err := kcp.ListenWithOptions(":10000", nil, 10, 3)
```

## Performance
```
  型号名称：	MacBook Pro
  型号标识符：	MacBookPro12,1
  处理器名称：	Intel Core i5
  处理器速度：	2.7 GHz
  处理器数目：	1
  核总数：	2
  L2 缓存（每个核）：	256 KB
  L3 缓存：	3 MB
  内存：	8 GB
```
```
$ go test -run Speed
new client 127.0.0.1:61165
total recv: 16777216
time for 16MB rtt with encryption 570.41176ms
&{BytesSent:33554432 BytesReceived:33554432 MaxConn:2 ActiveOpens:1 PassiveOpens:1 CurrEstab:1 InErrs:0 InCsumErrors:0 InSegs:42577 OutSegs:42641 OutBytes:48111336 RetransSegs:92 FastRetransSegs:92 LostSegs:0 RepeatSegs:0 FECRecovered:1 FECErrs:0 FECSegs:8514}
PASS
ok  	github.com/xtaci/kcp-go	0.600s
```

## Tuning

Q: I'm running > 3000 connections on my server. the CPU utilization is high.

A: A standalone `agent` or `gate` server for kcp-go is suggested, not only for CPU utilization, but also important to the **precision** of RTT measurements which indirectly affects retransmission. By increasing update `interval` with `SetNoDelay` like `conn.SetNoDelay(1, 40, 1, 1)` will dramatically reduce system load.

## Who is using this?

1. https://github.com/xtaci/kcptun -- A Secure Tunnel Based On KCP over UDP.
2. https://github.com/getlantern/lantern -- Lantern delivers fast access to the open Internet. 
3. https://github.com/smallnest/rpcx -- A RPC service framework based on net/rpc like alibaba Dubbo and weibo Motan.
4. https://github.com/gonet2/agent -- A gateway for games with stream multiplexing.

## Links

1. https://github.com/xtaci/libkcp -- FEC enhanced KCP session library for iOS/Android in C++
2. https://github.com/skywind3000/kcp -- A Fast and Reliable ARQ Protocol
3. https://github.com/klauspost/reedsolomon -- Reed-Solomon Erasure Coding in Go

### Support

You can support this project by the following methods:

1. Vultr promotion code:     
http://www.vultr.com/?ref=6897065

2. Paypal    
https://www.paypal.me/xtaci

Your name or github name will be listed on this page by default.
