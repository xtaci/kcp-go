<img src="assets/kcp-go.png" alt="kcp-go" height="100px" />


[![GoDoc][1]][2] [![Powered][9]][10] [![MIT licensed][11]][12] [![Build Status][3]][4] [![Go Report Card][5]][6] [![Coverage Status][7]][8] [![Sourcegraph][13]][14]

[1]: https://godoc.org/github.com/xtaci/kcp-go?status.svg
[2]: https://pkg.go.dev/github.com/xtaci/kcp-go
[3]: https://img.shields.io/github/created-at/xtaci/kcp-go
[4]: https://img.shields.io/github/created-at/xtaci/kcp-go
[5]: https://goreportcard.com/badge/github.com/xtaci/kcp-go
[6]: https://goreportcard.com/report/github.com/xtaci/kcp-go
[7]: https://codecov.io/gh/xtaci/kcp-go/branch/master/graph/badge.svg
[8]: https://codecov.io/gh/xtaci/kcp-go
[9]: https://img.shields.io/badge/KCP-Powered-blue.svg
[10]: https://github.com/skywind3000/kcp
[11]: https://img.shields.io/badge/license-MIT-blue.svg
[12]: LICENSE
[13]: https://sourcegraph.com/github.com/xtaci/kcp-go/-/badge.svg
[14]: https://sourcegraph.com/github.com/xtaci/kcp-go?badge

[English](README.md) | [中文](README_zh.md)

## 简介

**kcp-go** 是一个用于 [Go 语言](https://golang.org/) 的 **可靠 UDP (Reliable-UDP)** 库。

该库在 **UDP** 之上提供 **平滑、弹性、有序、错误检查和匿名** 的流式传输。经过开源项目 [kcptun](https://github.com/xtaci/kcptun) 的实战检验，从低端 MIPS 路由器到高端服务器，数以百万计的设备在各种应用中部署了由 kcp-go 驱动的程序，包括 **在线游戏、直播、文件同步和网络加速**。

[最新发布](https://github.com/xtaci/kcp-go/releases)

## 特性

1. 专为 **对延迟敏感** 的场景优化。
2. 采用 **缓存友好** 和 **内存优化** 设计，核心性能极高。
3. 单台商用服务器轻松支撑 **>5K 并发连接**。
4. 完全兼容 [net.Conn](https://golang.org/pkg/net/#Conn) 和 [net.Listener](https://golang.org/pkg/net/#Listener)，可直接替代 [net.TCPConn](https://golang.org/pkg/net/#TCPConn) 使用。
5. 支持使用 [Reed-Solomon Codes](https://en.wikipedia.org/wiki/Reed%E2%80%93Solomon_error_correction) 的 [FEC (前向纠错)](https://en.wikipedia.org/wiki/Forward_error_correction)。
6. 支持数据包级加密，包括 [AES](https://en.wikipedia.org/wiki/Advanced_Encryption_Standard)、[TEA](https://en.wikipedia.org/wiki/Tiny_Encryption_Algorithm)、[3DES](https://en.wikipedia.org/wiki/Triple_DES)、[Blowfish](https://en.wikipedia.org/wiki/Blowfish_(cipher))、[Cast5](https://en.wikipedia.org/wiki/CAST-128)、[Salsa20](https://en.wikipedia.org/wiki/Salsa20) 等，采用 [CFB 模式](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation#Cipher_Feedback_(CFB))，确保数据包完全匿名。
7. 支持 [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) 数据包加密。
8. 服务端应用仅创建 **固定数量的 goroutine**，极大降低了 **上下文切换** 开销。
9. 兼容 [skywind3000](https://github.com/skywind3000) 的 C 版本，并进行了多项改进。
10. 针对特定平台的优化：Linux 上的 [sendmmsg](http://man7.org/linux/man-pages/man2/sendmmsg.2.html) 和 [recvmmsg](http://man7.org/linux/man-pages/man2/recvmmsg.2.html)。

## 文档

有关完整文档，请参阅关联的 [Godoc](https://godoc.org/github.com/xtaci/kcp-go)。


### KCP-GO 分层模型

<img src="assets/layermodel.jpg" alt="layer-model" height="300px" />

## 协议规范

<img src="assets/frame.png" alt="Frame Format" height="109px" />

```
NONCE:
  16bytes cryptographically secure random number, nonce changes for every packet.
  
CRC32:
  CRC-32 checksum of data using the IEEE polynomial
 
FEC TYPE:
  typeData = 0xF1
  typeParity = 0xF2
  
FEC SEQID:
  monotonically increasing in range: [0, (0xffffffff/shardSize) * shardSize - 1]
  
SIZE:
  The size of KCP frame plus 2

KCP Header
+------------------+
| conv      uint32 |
+------------------+
| cmd       uint8  |
+------------------+
| frg       uint8  |
+------------------+
| wnd      uint16  |
+------------------+
| ts       uint32  |
+------------------+
| sn       uint32  |
+------------------+
| una      uint32  |
+------------------+
| data     []byte  |
+------------------+
```

## 性能
```
2025/11/26 11:12:51 beginning tests, encryption:salsa20, fec:10/3
goos: linux
goarch: amd64
pkg: github.com/xtaci/kcp-go/v5
cpu: AMD Ryzen 9 5950X 16-Core Processor
BenchmarkSM4
BenchmarkSM4-32                            56077             21672 ns/op         138.43 MB/s           0 B/op          0 allocs/op
BenchmarkAES128
BenchmarkAES128-32                        525854              2228 ns/op        1346.69 MB/s           0 B/op          0 allocs/op
BenchmarkAES192
BenchmarkAES192-32                        473692              2429 ns/op        1234.95 MB/s           0 B/op          0 allocs/op
BenchmarkAES256
BenchmarkAES256-32                        427497              2725 ns/op        1101.06 MB/s           0 B/op          0 allocs/op
BenchmarkTEA
BenchmarkTEA-32                           149976              8085 ns/op         371.06 MB/s           0 B/op          0 allocs/op
BenchmarkXOR
BenchmarkXOR-32                         12333190                92.35 ns/op     32485.16 MB/s          0 B/op          0 allocs/op
BenchmarkBlowfish
BenchmarkBlowfish-32                       70762             16983 ns/op         176.65 MB/s           0 B/op          0 allocs/op
BenchmarkNone
BenchmarkNone-32                        47325206                24.49 ns/op     122482.39 MB/s         0 B/op          0 allocs/op
BenchmarkCast5
BenchmarkCast5-32                          66837             18035 ns/op         166.35 MB/s           0 B/op          0 allocs/op
Benchmark3DES
Benchmark3DES-32                           18402             64349 ns/op          46.62 MB/s           0 B/op          0 allocs/op
BenchmarkTwofish
BenchmarkTwofish-32                        56440             21380 ns/op         140.32 MB/s           0 B/op          0 allocs/op
BenchmarkXTEA
BenchmarkXTEA-32                           45616             26124 ns/op         114.84 MB/s           0 B/op          0 allocs/op
BenchmarkSalsa20
BenchmarkSalsa20-32                       525685              2199 ns/op        1363.97 MB/s           0 B/op          0 allocs/op
BenchmarkCRC32
BenchmarkCRC32-32                       19418395                59.05 ns/op     17341.83 MB/s
BenchmarkCsprngSystem
BenchmarkCsprngSystem-32                 2912889               404.3 ns/op        39.58 MB/s
BenchmarkCsprngMD5
BenchmarkCsprngMD5-32                   15063580                79.23 ns/op      201.95 MB/s
BenchmarkCsprngSHA1
BenchmarkCsprngSHA1-32                  20186407                60.04 ns/op      333.08 MB/s
BenchmarkCsprngNonceMD5
BenchmarkCsprngNonceMD5-32              13863704                85.11 ns/op      187.98 MB/s
BenchmarkCsprngNonceAES128
BenchmarkCsprngNonceAES128-32           97239751                12.56 ns/op     1274.09 MB/s
BenchmarkFECDecode
BenchmarkFECDecode-32                    1808791               679.1 ns/op      2208.94 MB/s        1641 B/op          3 allocs/op
BenchmarkFECEncode
BenchmarkFECEncode-32                    6671982               181.4 ns/op      8270.76 MB/s           2 B/op          0 allocs/op
BenchmarkFlush
BenchmarkFlush-32                         322982              3809 ns/op               0 B/op          0 allocs/op
BenchmarkDebugLog
BenchmarkDebugLog-32                    1000000000               0.2146 ns/op
BenchmarkEchoSpeed4K
BenchmarkEchoSpeed4K-32                    35583             32875 ns/op         124.59 MB/s       18223 B/op        148 allocs/op
BenchmarkEchoSpeed64K
BenchmarkEchoSpeed64K-32                    1995            510301 ns/op         128.43 MB/s      284233 B/op       2297 allocs/op
BenchmarkEchoSpeed512K
BenchmarkEchoSpeed512K-32                    259           4058131 ns/op         129.19 MB/s     2243058 B/op      18148 allocs/op
BenchmarkEchoSpeed1M
BenchmarkEchoSpeed1M-32                      145           8561996 ns/op         122.47 MB/s     4464227 B/op      36009 allocs/op
BenchmarkSinkSpeed4K
BenchmarkSinkSpeed4K-32                   194648             42136 ns/op          97.21 MB/s        2073 B/op         50 allocs/op
BenchmarkSinkSpeed64K
BenchmarkSinkSpeed64K-32                   10000            113038 ns/op         579.77 MB/s       29242 B/op        741 allocs/op
BenchmarkSinkSpeed256K
BenchmarkSinkSpeed256K-32                   1555            843724 ns/op         621.40 MB/s      229558 B/op       5850 allocs/op
BenchmarkSinkSpeed1M
BenchmarkSinkSpeed1M-32                      667           1783214 ns/op         588.03 MB/s      462691 B/op      11694 allocs/op
PASS
ok      github.com/xtaci/kcp-go/v5      49.978s
```

```
===
Model Name:	MacBook Pro
Model Identifier:	MacBookPro14,1
Processor Name:	Intel Core i5
Processor Speed:	3.1 GHz
Number of Processors:	1
Total Number of Cores:	2
L2 Cache (per Core):	256 KB
L3 Cache:	4 MB
Memory:	8 GB
===

$ go test -v -run=^$ -bench .
beginning tests, encryption:salsa20, fec:10/3
goos: darwin
goarch: amd64
pkg: github.com/xtaci/kcp-go
BenchmarkSM4-4                 	   50000	     32180 ns/op	  93.23 MB/s	       0 B/op	       0 allocs/op
BenchmarkAES128-4              	  500000	      3285 ns/op	 913.21 MB/s	       0 B/op	       0 allocs/op
BenchmarkAES192-4              	  300000	      3623 ns/op	 827.85 MB/s	       0 B/op	       0 allocs/op
BenchmarkAES256-4              	  300000	      3874 ns/op	 774.20 MB/s	       0 B/op	       0 allocs/op
BenchmarkTEA-4                 	  100000	     15384 ns/op	 195.00 MB/s	       0 B/op	       0 allocs/op
BenchmarkXOR-4                 	20000000	        89.9 ns/op	33372.00 MB/s	       0 B/op	       0 allocs/op
BenchmarkBlowfish-4            	   50000	     26927 ns/op	 111.41 MB/s	       0 B/op	       0 allocs/op
BenchmarkNone-4                	30000000	        45.7 ns/op	65597.94 MB/s	       0 B/op	       0 allocs/op
BenchmarkCast5-4               	   50000	     34258 ns/op	  87.57 MB/s	       0 B/op	       0 allocs/op
Benchmark3DES-4                	   10000	    117149 ns/op	  25.61 MB/s	       0 B/op	       0 allocs/op
BenchmarkTwofish-4             	   50000	     33538 ns/op	  89.45 MB/s	       0 B/op	       0 allocs/op
BenchmarkXTEA-4                	   30000	     45666 ns/op	  65.69 MB/s	       0 B/op	       0 allocs/op
BenchmarkSalsa20-4             	  500000	      3308 ns/op	 906.76 MB/s	       0 B/op	       0 allocs/op
BenchmarkCRC32-4               	20000000	        65.2 ns/op	15712.43 MB/s
BenchmarkCsprngSystem-4        	 1000000	      1150 ns/op	  13.91 MB/s
BenchmarkCsprngMD5-4           	10000000	       145 ns/op	 110.26 MB/s
BenchmarkCsprngSHA1-4          	10000000	       158 ns/op	 126.54 MB/s
BenchmarkCsprngNonceMD5-4      	10000000	       153 ns/op	 104.22 MB/s
BenchmarkCsprngNonceAES128-4   	100000000	        19.1 ns/op	 837.81 MB/s
BenchmarkFECDecode-4           	 1000000	      1119 ns/op	1339.61 MB/s	    1606 B/op	       2 allocs/op
BenchmarkFECEncode-4           	 2000000	       832 ns/op	1801.83 MB/s	      17 B/op	       0 allocs/op
BenchmarkFlush-4               	 5000000	       272 ns/op	       0 B/op	       0 allocs/op
BenchmarkEchoSpeed4K-4         	    5000	    259617 ns/op	  15.78 MB/s	    5451 B/op	     149 allocs/op
BenchmarkEchoSpeed64K-4        	    1000	   1706084 ns/op	  38.41 MB/s	   56002 B/op	    1604 allocs/op
BenchmarkEchoSpeed512K-4       	     100	  14345505 ns/op	  36.55 MB/s	  482597 B/op	   13045 allocs/op
BenchmarkEchoSpeed1M-4         	      30	  34859104 ns/op	  30.08 MB/s	 1143773 B/op	   27186 allocs/op
BenchmarkSinkSpeed4K-4         	   50000	     31369 ns/op	 130.57 MB/s	    1566 B/op	      30 allocs/op
BenchmarkSinkSpeed64K-4        	    5000	    329065 ns/op	 199.16 MB/s	   21529 B/op	     453 allocs/op
BenchmarkSinkSpeed256K-4       	     500	   2373354 ns/op	 220.91 MB/s	  166332 B/op	    3554 allocs/op
BenchmarkSinkSpeed1M-4         	     300	   5117927 ns/op	 204.88 MB/s	  310378 B/op	    6988 allocs/op
PASS
ok  	github.com/xtaci/kcp-go	50.349s
```

```
=== Raspberry Pi 4 ===

➜  kcp-go git:(master) cat /proc/cpuinfo
processor	: 0
model name	: ARMv7 Processor rev 3 (v7l)
BogoMIPS	: 108.00
Features	: half thumb fastmult vfp edsp neon vfpv3 tls vfpv4 idiva idivt vfpd32 lpae evtstrm crc32
CPU implementer	: 0x41
CPU architecture: 7
CPU variant	: 0x0
CPU part	: 0xd08
CPU revision	: 3

➜  kcp-go git:(master)  go test -run=^$ -bench .
2020/01/05 19:25:13 beginning tests, encryption:salsa20, fec:10/3
goos: linux
goarch: arm
pkg: github.com/xtaci/kcp-go/v5
BenchmarkSM4-4                     20000             86475 ns/op          34.69 MB/s           0 B/op          0 allocs/op
BenchmarkAES128-4                  20000             62254 ns/op          48.19 MB/s           0 B/op          0 allocs/op
BenchmarkAES192-4                  20000             71802 ns/op          41.78 MB/s           0 B/op          0 allocs/op
BenchmarkAES256-4                  20000             80570 ns/op          37.23 MB/s           0 B/op          0 allocs/op
BenchmarkTEA-4                     50000             37343 ns/op          80.34 MB/s           0 B/op          0 allocs/op
BenchmarkXOR-4                    100000             22266 ns/op         134.73 MB/s           0 B/op          0 allocs/op
BenchmarkBlowfish-4                20000             66123 ns/op          45.37 MB/s           0 B/op          0 allocs/op
BenchmarkNone-4                  3000000               518 ns/op        5786.77 MB/s           0 B/op          0 allocs/op
BenchmarkCast5-4                   20000             76705 ns/op          39.11 MB/s           0 B/op          0 allocs/op
Benchmark3DES-4                     5000            418868 ns/op           7.16 MB/s           0 B/op          0 allocs/op
BenchmarkTwofish-4                  5000            326896 ns/op           9.18 MB/s           0 B/op          0 allocs/op
BenchmarkXTEA-4                    10000            114418 ns/op          26.22 MB/s           0 B/op          0 allocs/op
BenchmarkSalsa20-4                 50000             36736 ns/op          81.66 MB/s           0 B/op          0 allocs/op
BenchmarkCRC32-4                 1000000              1735 ns/op         589.98 MB/s
BenchmarkCsprngSystem-4          1000000              2179 ns/op           7.34 MB/s
BenchmarkCsprngMD5-4             2000000               811 ns/op          19.71 MB/s
BenchmarkCsprngSHA1-4            2000000               862 ns/op          23.19 MB/s
BenchmarkCsprngNonceMD5-4        2000000               878 ns/op          18.22 MB/s
BenchmarkCsprngNonceAES128-4     5000000               326 ns/op          48.97 MB/s
BenchmarkFECDecode-4              200000              9081 ns/op         165.16 MB/s         140 B/op          1 allocs/op
BenchmarkFECEncode-4              100000             12039 ns/op         124.59 MB/s          11 B/op          0 allocs/op
BenchmarkFlush-4                  100000             21704 ns/op               0 B/op          0 allocs/op
BenchmarkEchoSpeed4K-4              2000            981182 ns/op           4.17 MB/s       12384 B/op        424 allocs/op
BenchmarkEchoSpeed64K-4              100          10503324 ns/op           6.24 MB/s      123616 B/op       3779 allocs/op
BenchmarkEchoSpeed512K-4              20         138633802 ns/op           3.78 MB/s     1606584 B/op      29233 allocs/op
BenchmarkEchoSpeed1M-4                 5         372903568 ns/op           2.81 MB/s     4080504 B/op      63600 allocs/op
BenchmarkSinkSpeed4K-4             10000            121239 ns/op          33.78 MB/s        4647 B/op        104 allocs/op
BenchmarkSinkSpeed64K-4             1000           1587906 ns/op          41.27 MB/s       50914 B/op       1115 allocs/op
BenchmarkSinkSpeed256K-4             100          16277830 ns/op          32.21 MB/s      453027 B/op       9296 allocs/op
BenchmarkSinkSpeed1M-4               100          31040703 ns/op          33.78 MB/s      898097 B/op      18932 allocs/op
PASS
ok      github.com/xtaci/kcp-go/v5      64.151s
```


## 典型火焰图
![Flame Graph in kcptun](assets/flame.png)

## 关键设计考量

### 1. 切片 (Slice) vs. 容器/链表 (Container/List)

`kcp.flush()` 每 20 毫秒循环遍历发送队列以进行重传检查。

通过基准测试对比了顺序遍历 *切片* 与 *容器/链表* 的性能，代码在 [这里](https://github.com/xtaci/notes/blob/master/golang/benchmark2/cachemiss_test.go)：

```
BenchmarkLoopSlice-4   	2000000000	         0.39 ns/op
BenchmarkLoopList-4    	100000000	        54.6 ns/op
```

相比切片，链表结构会导致 **严重的缓存未命中 (cache misses)**，而切片则具有更好的 **局部性 (locality)**。对于 5,000 个连接，窗口大小为 32，间隔为 20 毫秒，使用切片每次 `kcp.flush()` 消耗 6 微秒 (0.03% CPU)，而使用链表则消耗 8.7 毫秒 (43.5% CPU)。

### 2. 计时精度 vs. 系统调用 clock_gettime

计时精度对于 **RTT 估算** 至关重要。计时不准会导致 KCP 发生不必要的重传，但调用 `time.Now()` 需要 42 个周期（在 4 GHz CPU 上为 10.5 ns，在我的 MacBook Pro 2.7 GHz 上为 15.6 ns）。

`time.Now()` 的基准测试在 [这里](https://github.com/xtaci/notes/blob/master/golang/benchmark2/syscall_test.go)：

```
BenchmarkNow-4         	100000000	        15.6 ns/op
```

kcp-go 优化了时间获取策略：每次 `kcp.output()` 调用返回时更新当前时间。在单次 `kcp.flush()` 操作中，仅查询一次系统时间。对于 5,000 个连接，这消耗 5000 × 15.6 ns = 78 μs（当没有数据包需要发送时的固定成本）。对于 10 MB/s 的数据传输（MTU 为 1400），`kcp.output()` 每秒大约被调用 7,500 次，`time.Now()` 每秒消耗 117 μs。

### 3. 内存管理

内存分配主要依赖全局缓冲池 `xmit.Buf`。kcp-go 需要分配内存时，会从池中获取固定容量（1500字节，即 mtuLimit）的缓冲区。RX、TX 及 FEC 队列均复用该池中的缓冲区，使用完毕后归还，避免了不必要的内存清零开销。该机制维持了切片对象的高水位线，既保证传输中的对象不被 GC 回收，又能在空闲时将内存归还给运行时环境。

### 4. 信息安全

kcp-go 内置了多种块加密算法支持的数据包加密功能，采用 [CFB 模式](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation#Cipher_Feedback_(CFB)) 运行。每个数据包的加密过程都始于加密一个源自 [系统熵](https://en.wikipedia.org/wiki//dev/random) 的 [nonce](https://en.wikipedia.org/wiki/Cryptographic_nonce)，确保即使明文相同，生成的密文也绝不重复。

加密后的数据包完全匿名，涵盖头部（FEC, KCP）、校验和及载荷。请注意，如果禁用底层加密，即便上层应用了加密，传输过程仍是不安全的。因为协议头部是 ***明文*** 的，易受篡改攻击（如修改 *滑动窗口大小*、*RTT*、*FEC 参数* 和 *校验和*）。建议使用 `AES-128` 进行最小程度的加密，因为现代 CPU 具有 [AES-NI](https://en.wikipedia.org/wiki/AES_instruction_set) 指令，性能优于 `salsa20`（见上表）。

针对 kcp-go 的其他可能攻击包括：

- **[流量分析](https://en.wikipedia.org/wiki/Traffic_analysis):** 特定网站上的数据流在数据交换期间可能会表现出模式。通过采用 [smux](https://github.com/xtaci/smux) 混合数据流并引入噪声，这种类型的窃听已得到缓解。虽然尚未出现完美的解决方案，但理论上，在更大规模的网络上混洗/混合消息可以缓解此问题。
- **[重放攻击](https://en.wikipedia.org/wiki/Replay_attack):** 由于 kcp-go 尚未引入非对称加密，因此捕获数据包并在另一台机器上重放是可能的。请注意，劫持会话并解密内容仍然是 *不可能的*。上层应使用非对称加密系统来保证每条消息的真实性（以确保每条消息仅被处理一次），例如 HTTPS/OpenSSL/LibreSSL。使用私钥对请求进行签名可以消除此类攻击。

### 5. 报文时钟

1. **FastACK 立即发送**：FastACK 触发后立即发送，不再等待固定的 interval。
2. **ACK 立即发送**：累计到一个 ACK 后立即发送，同样不等待 interval。
   在高速网络中，这相当于提供更高频率的“时钟信号”，可使单向传输速度提升约 6 倍。例如，高速链路上一个 batch 只需 1.5ms 就能处理完成，如果仍遵循固定 10ms 的发送周期，那么实际吞吐将仅剩 1/6。
3. **Pacing 机制**：引入 Pacing 时钟，防止在较大的 snd_wnd 下数据瞬间堆积到内核导致突发拥塞和丢包。用户态实现 Pacing 较难，目前勉强实现了一个可用版本，用户态 echo 测试能稳定在 100MB/s 以上。
4. **数据结构优化**：优化数据结构（如 snd_buf 的 ringbuffer），保证结构具备良好的缓存一致性 (cache coherency)。队列不宜过长，否则遍历开销会引入额外延迟。在高速网络下，应将 BDP 对应的 buffer 设得更小，以此降低数据结构带来的 latency。注意：现有 KCP 结构的 RTO 计算复杂度为 O(n)，若要改为 O(1) 则需要大幅重构。

说到底，传输系统中没有任何东西比时钟（实时性）更重要。

## 连接终止

KCP 协议 **未定义** 类似 TCP 的 **SYN/FIN/RST** 控制消息。因此，您需要在应用层自行实现 **keepalive/heartbeat (心跳/保活) 机制**。一个实际的例子是在会话之上使用 **多路复用** 协议，例如 [smux](https://github.com/xtaci/smux)（它具有嵌入式的 keepalive 机制）。参考实现请参见 [kcptun](https://github.com/xtaci/kcptun)。

## 常见问题 (FAQ)

**Q: 我的服务器正在处理 >5K 连接，CPU 利用率非常高。**

**A:** 建议将 kcp-go 部署在独立的 `agent` 或 `gate` 服务器上。这不仅能降低 CPU 负载，还能提高 RTT 测量（计时）的 **精度**，从而优化重传机制。使用 `SetNoDelay` 增加更新 `interval`，例如 `conn.SetNoDelay(1, 40, 1, 1)`，将显着降低系统负载，但可能会降低性能。

**Q: 我应该何时启用 FEC？**

**A:** 对于长距离传输，前向纠错（FEC）至关重要，因为丢包会导致严重的延迟惩罚。在现代世界复杂的数据包路由网络中，基于往返时间的丢包检查并不总是有效的。长距离传输中 RTT 样本的显著偏差通常会导致典型 RTT 估算器中的 RTO 值较大，从而减慢传输速度。

**Q: 我应该启用加密吗？**

**A:** 是的，为了协议的安全性，即使上层已经有加密。

## 谁在使用？

1. https://github.com/xtaci/kcptun -- 基于 KCP over UDP 的安全隧道。
2. https://github.com/getlantern/lantern -- Lantern 提供快速访问开放互联网的服务。
3. https://github.com/smallnest/rpcx -- 基于 net/rpc 的 RPC 服务框架，类似于阿里巴巴 Dubbo 和微博 Motan。
4. https://github.com/gonet2/agent -- 带有流多路复用的游戏网关。
5. https://github.com/syncthing/syncthing -- 开源持续文件同步。

### 寻找 C++ 客户端？
1. https://github.com/xtaci/libkcp -- 用于 iOS/Android 的 C++ FEC 增强 KCP 会话库

## 示例

1. [简单示例](https://github.com/xtaci/kcp-go/tree/master/examples)
2. [kcptun 客户端](https://github.com/xtaci/kcptun/blob/master/client/main.go)
3. [kcptun 服务端](https://github.com/xtaci/kcptun/blob/master/server/main.go)

## 相关链接

1. https://github.com/xtaci/smux/ -- 内存占用极少的 golang 流多路复用库
1. **https://github.com/xtaci/libkcp -- 用于 iOS/Android 的 C++ FEC 增强 KCP 会话库**
1. https://github.com/skywind3000/kcp -- 快速可靠的 ARQ 协议
1. https://github.com/klauspost/reedsolomon -- Go 语言实现的 Reed-Solomon 纠删码
