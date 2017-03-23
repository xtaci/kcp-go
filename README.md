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

> *(Claude Elwood Shannon)*

## Introduction

**kcp-go** is a ***Production-Grade Reliable-UDP*** library for [golang](https://golang.org/). 

It provides ***fast, ordered and error-checked*** delivery of streams over **UDP** packets, has been well tested with opensource project [kcptun](https://github.com/xtaci/kcptun). Millions of devices(from low-end MIPS routers to high-end servers) are running with **kcp-go** at present, including applications like **online games, live broadcasting, file synchronization and network acceleration**.

[Lastest Release](https://github.com/xtaci/kcp-go/releases)

## Features

1. Optimized for ***Realtime Multiplayer Games, Audio/Video Streaming***.
1. Compatible with [skywind3000's](https://github.com/skywind3000) C version with language specific optimizations.
1. ***Cache friendly*** and ***Memory optimized*** design, offers extremely ***High Performance*** core.
1. Compatible with [net.Conn](https://golang.org/pkg/net/#Conn) and [net.Listener](https://golang.org/pkg/net/#Listener), easy to use.
1. [FEC(Forward Error Correction)](https://en.wikipedia.org/wiki/Forward_error_correction) Support with [Reed-Solomon Codes](https://en.wikipedia.org/wiki/Reed%E2%80%93Solomon_error_correction)
1. Packet level encryption support with [AES](https://en.wikipedia.org/wiki/Advanced_Encryption_Standard), [TEA](https://en.wikipedia.org/wiki/Tiny_Encryption_Algorithm), [3DES](https://en.wikipedia.org/wiki/Triple_DES), [Blowfish](https://en.wikipedia.org/wiki/Blowfish_(cipher)), [Cast5](https://en.wikipedia.org/wiki/CAST-128), [Salsa20]( https://en.wikipedia.org/wiki/Salsa20), etc. in [CFB](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation#Cipher_Feedback_.28CFB.29) mode.
1. ***O(1) goroutines*** created for the entire server application, minimized goroutine context switch.

## Conventions

Control messages like **SYN/FIN/RST** in TCP ***are not defined*** in KCP, you need some ***keepalive/heartbeat mechanism*** in the application-level. A real world example is to use some ***multiplexing*** protocol over session, such as [smux](https://github.com/xtaci/smux)(with embeded keepalive mechanism), see [kcptun](https://github.com/xtaci/kcptun) for example.

## Documentation

For complete documentation, see the associated [Godoc](https://godoc.org/github.com/xtaci/kcp-go).

## Specification

<img src="frame.png" alt="Frame Format" height="109px" />

```
+-----------------+
| SESSION         |
+-----------------+
| KCP(ARQ)        |
+-----------------+
| FEC(OPTIONAL)   |
+-----------------+
| CRYPTO(OPTIONAL)|
+-----------------+
| UDP(PACKET)     |
+-----------------+
| IP              |
+-----------------+
| LINK            |
+-----------------+
| PHY             |
+-----------------+
(LAYER MODEL OF KCP-GO)
```


## Usage

Client:   [full demo](https://github.com/xtaci/kcptun/blob/master/client/main.go)
```go
kcpconn, err := kcp.DialWithOptions("192.168.0.1:10000", nil, 10, 3)
```
Server:   [full demo](https://github.com/xtaci/kcptun/blob/master/server/main.go)
```go
lis, err := kcp.ListenWithOptions(":10000", nil, 10, 3)
```

## Performance
```
  Model Name:	MacBook Pro
  Model Identifier:	MacBookPro12,1
  Processor Name:	Intel Core i5
  Processor Speed:	2.7 GHz
  Number of Processors:	1
  Total Number of Cores:	2
  L2 Cache (per Core):	256 KB
  L3 Cache:	3 MB
  Memory:	8 GB
```
```
$ go test -run=^$ -bench .
beginning tests, encryption:salsa20, fec:10/3
BenchmarkAES128-4          	  200000	      8455 ns/op	 354.81 MB/s	       0 B/op	       0 allocs/op
BenchmarkAES192-4          	  200000	      9370 ns/op	 320.16 MB/s	       0 B/op	       0 allocs/op
BenchmarkAES256-4          	  200000	     10262 ns/op	 292.32 MB/s	       0 B/op	       0 allocs/op
BenchmarkTEA-4             	  100000	     18747 ns/op	 160.02 MB/s	       0 B/op	       0 allocs/op
BenchmarkXOR-4             	 5000000	       319 ns/op	9379.21 MB/s	       0 B/op	       0 allocs/op
BenchmarkBlowfish-4        	   50000	     35845 ns/op	  83.69 MB/s	       0 B/op	       0 allocs/op
BenchmarkNone-4            	30000000	        58.5 ns/op	51252.66 MB/s	       0 B/op	       0 allocs/op
BenchmarkCast5-4           	   30000	     46058 ns/op	  65.14 MB/s	       0 B/op	       0 allocs/op
Benchmark3DES-4            	    2000	    650290 ns/op	   4.61 MB/s	       6 B/op	       0 allocs/op
BenchmarkTwofish-4         	   30000	     44042 ns/op	  68.12 MB/s	       0 B/op	       0 allocs/op
BenchmarkXTEA-4            	   30000	     59862 ns/op	  50.11 MB/s	       0 B/op	       0 allocs/op
BenchmarkSalsa20-4         	  300000	      4045 ns/op	 741.50 MB/s	       0 B/op	       0 allocs/op
BenchmarkEchoSpeed4K-4     	    5000	    286664 ns/op	  14.29 MB/s	    5534 B/op	     174 allocs/op
BenchmarkEchoSpeed64K-4    	     500	   3026142 ns/op	  21.66 MB/s	   69494 B/op	    2132 allocs/op
BenchmarkEchoSpeed512K-4   	      50	  26078786 ns/op	  20.10 MB/s	  630231 B/op	   16933 allocs/op
BenchmarkEchoSpeed1M-4     	      20	  75141637 ns/op	  13.95 MB/s	 1671859 B/op	   44578 allocs/op
BenchmarkSinkSpeed4K-4     	   10000	    147721 ns/op	  27.73 MB/s	    4206 B/op	      84 allocs/op
BenchmarkSinkSpeed64K-4    	    1000	   1033438 ns/op	  63.42 MB/s	   40962 B/op	     954 allocs/op
BenchmarkSinkSpeed256K-4   	     200	   7336797 ns/op	  71.46 MB/s	  272973 B/op	    6839 allocs/op
BenchmarkSinkSpeed1M-4     	     100	  14701943 ns/op	  71.32 MB/s	  614016 B/op	   13549 allocs/op
PASS
ok  	github.com/xtaci/kcp-go	36.141s
```

## Design Considerations

1. slice vs. container/list

`kcp.flush()` loops through the send queue for retransmission checking for every 20ms(interval).

I've wrote a benchmark for comparing sequential loop through *slice* and *container/list* here:

https://github.com/xtaci/notes/blob/master/golang/benchmark2/cachemiss_test.go

```
BenchmarkLoopSlice-4   	2000000000	         0.39 ns/op
BenchmarkLoopList-4    	100000000	        54.6 ns/op
```

List structure introduces **heavy cache misses** compared to slice which owns better **locality**, 5000 connections with 32 window size and 20ms interval will cost 6us/0.03%(cpu) using slice, and 8.7ms/43.5%(cpu) for list for each `kcp.flush()`.

2. Timing accuracy vs. syscall clock_gettime

Timing is **critical** to **RTT estimator**, inaccurate timing introduces false retransmissions in KCP, but calling `time.Now()` costs 42 cycles(10.5ns on 4GHz CPU, 15.6ns on my MacBook Pro 2.7GHz), the benchmark for time.Now():

https://github.com/xtaci/notes/blob/master/golang/benchmark2/syscall_test.go

```
BenchmarkNow-4         	100000000	        15.6 ns/op
```

In kcp-go, after each `kcp.output()` function call, current time will be updated upon return, and each `kcp.flush()` will get current time once. For most of the time, 5000 connections costs 5000 * 15.6ns = 78us(no packet needs to be sent by `kcp.output()`), as for 10MB/s data transfering with 1400 MTU, `kcp.output()` will be called around 7500 times and costs 117us for `time.Now()` in **every second**.


## Tuning

Q: I'm running > 3000 connections on my server. the CPU utilization is high.

A: A standalone `agent` or `gate` server for kcp-go is suggested, not only for CPU utilization, but also important to the **precision** of RTT measurements which indirectly affects retransmission. By increasing update `interval` with `SetNoDelay` like `conn.SetNoDelay(1, 40, 1, 1)` will dramatically reduce system load.

## Who is using this?

1. https://github.com/xtaci/kcptun -- A Secure Tunnel Based On KCP over UDP.
2. https://github.com/getlantern/lantern -- Lantern delivers fast access to the open Internet. 
3. https://github.com/smallnest/rpcx -- A RPC service framework based on net/rpc like alibaba Dubbo and weibo Motan.
4. https://github.com/gonet2/agent -- A gateway for games with stream multiplexing.
5. https://github.com/syncthing/syncthing -- Open Source Continuous File Synchronization.

## Links

1. https://github.com/xtaci/libkcp -- FEC enhanced KCP session library for iOS/Android in C++
2. https://github.com/skywind3000/kcp -- A Fast and Reliable ARQ Protocol
3. https://github.com/klauspost/reedsolomon -- Reed-Solomon Erasure Coding in Go

### Support

You can support this project by the following methods:

1. Vultr promotion code:     
http://www.vultr.com/?ref=6897065

2. Paypal    
https://www.paypal.me/xtaci/5

3. 支付宝
hanbmei@163.com（接受在线付费咨询, Price: ¥128/45min, QQ:301008666，注明意图。）
