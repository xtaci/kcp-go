// +build go1.9

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	gouuid "github.com/satori/go.uuid"
)

type pingRequestData struct {
	RemoteIP string `json:"remote_ip"`
}

type pingResponseData struct {
	RemoteIP string      `json:"remote_ip"`
	FromIP   string      `json:"from_ip"`
	UUID     gouuid.UUID `json:"uuid"`
}

type pingpongTime struct {
	ping time.Time
	pong time.Time
}

type ipPingInfo struct {
	addr  *net.UDPAddr
	pingM map[int]*pingpongTime
	mutex sync.RWMutex
}

func (p *ipPingInfo) ping(idx int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	_, ok := p.pingM[idx]
	if ok {
		log.Fatalf("idx repeat. idx:%v \n", idx)
	}
	p.pingM[idx] = &pingpongTime{ping: time.Now()}
}

func (p *ipPingInfo) pong(idx int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	pp, ok := p.pingM[idx]
	if !ok {
		log.Fatalf("idx not found. idx:%v \n", idx)
	}
	pp.pong = time.Now()
}

func (p *ipPingInfo) cost(idx int) time.Duration {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	pp, ok := p.pingM[idx]
	if !ok {
		log.Fatalf("idx not found. idx:%v \n", idx)
	}
	if pp.pong.IsZero() {
		return 0
	}
	return pp.pong.Sub(pp.ping)
}

type udpHostManager struct {
	mutex sync.RWMutex
	conn  *net.UDPConn
	ipM   map[string]ipPingInfo
}

func NewUDPHostManager(ips []string, port int, intervalS int) (*udpHostManager, error) {
	hm := new(udpHostManager)
	hm.ipM = make(map[string]ipPingInfo)

	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	hm.conn = conn

	for _, ip := range ips {
		addr, err := net.ResolveUDPAddr("udp", ip+":"+fmt.Sprint(port))
		if err != nil {
			return nil, err
		}
		hm.ipM[ip] = ipPingInfo{pingM: make(map[int]*pingpongTime), addr: addr}
	}

	go hm.ping()
	go hm.update()

	return hm, nil
}

func (hm *udpHostManager) update() {
	for {
		hm.updateOnce()
	}
}

func (hm *udpHostManager) updateOnce() error {
	buf := make([]byte, 1500)

	n, _, err := hm.conn.ReadFrom(buf)
	if err != nil {
		log.Printf("ReadFrom err:%v \n", err)
		return err
	}

	var resp pingResponseData
	err = json.Unmarshal(buf[:n], &resp)
	if err != nil {
		log.Printf("Unmarshal err:%v \n", err)
		return err
	}

	hm.updateIp(resp.UUID, resp.FromIP, resp.RemoteIP)
	return nil
}

func (hm *udpHostManager) updateIp(uuid gouuid.UUID, fromIp, remoteIp string) {
	s := strings.Split(remoteIp, ":")
	if len(s) != 2 {
		log.Fatalf("remoteip not valid. remoteIp:%v \n", remoteIp)
	}
	h, ok := hm.ipM[s[0]]
	if !ok {
		log.Fatalf("ip not exist. ip:%v \n", s[0])
	}

	idx, err := strconv.Atoi(s[1])
	if err != nil {
		log.Fatalf("remoteip not valid. parse error. remoteIp:%v \n", remoteIp)
	}
	h.pong(idx)
}

func (hm *udpHostManager) ping() {
	for i := 0; i < 10000; i++ {
		hm.pingIps(i + 1)
		time.Sleep(time.Second)
		hm.statIps(i + 1)
	}
}

func (hm *udpHostManager) pingIps(idx int) {
	for ip, info := range hm.ipM {
		var req pingRequestData
		req.RemoteIP = ip + ":" + fmt.Sprint(idx)
		reqBuf, err := json.Marshal(req)
		if err != nil {
			log.Printf("Marshal ip:%v err:%v \n", ip, err)
			continue
		}

		_, err = hm.conn.WriteTo(reqBuf, info.addr)
		if err != nil {
			log.Printf("WriteTo ip:%v err:%v \n", ip, err)
		}
		info.ping(idx)
	}
}

func (hm *udpHostManager) statIps(idx int) {
	for ip, info := range hm.ipM {
		cost := info.cost(idx)

		if cost != 0 {
			log.Printf("ping:%v idx:%v cost:%v \n", ip, idx, cost)
		} else {
			log.Printf("ping:%v idx:%v miss \n", ip, idx)
		}
	}
	fmt.Printf("\n")
}

var targetAddrs = flag.String("targetAddrs", "", "target address")
var targetPort = flag.Int("targetPort", 0, "target port")

func main() {
	flag.Parse()

	log.Printf("targetAddrs:%v targetPort:%v \n", *targetAddrs, *targetPort)
	_, err := NewUDPHostManager(strings.Split(*targetAddrs, ","), *targetPort, 0)
	if err != nil {
		log.Fatalf("new udp host failed. err:%v \n", err)
	}

	ch := make(chan int, 1)
	<-ch
}
