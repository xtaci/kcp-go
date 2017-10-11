package kcp

import (
	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/layers"
	"fmt"
	"strconv"
	"net"
	"encoding/binary"
	"crypto/rand"
	mrand "math/rand"
	"time"
	"log"
	"github.com/pkg/errors"
	"runtime"
	"os/exec"
	"strings"
	"sort"
	"os"
)

const (
	fin = 1 << iota
	syn = 1 << iota
	rst = 1 << iota
	psh = 1 << iota
	ack = 1 << iota
	urg = 1 << iota
	ece = 1 << iota
	cwr = 1 << iota
)

type FakeTCP struct {
	send_handle        *pcap.Handle
	recv_handle        *pcap.Handle
	is_server          bool
	initialized        bool
	ch_initialized     chan bool
	ch_packets         chan gopacket.Packet
	ch_payload_packets chan packet_t

	local_mac          net.HardwareAddr
	local_ip           net.IP
	local_port         uint16
	gateway_mac        net.HardwareAddr
	gateway_ip         net.IP
	bing_ip            net.IP

	connections        map[string]*FakeTCPConn

	net.PacketConn
	Conn               net.Conn // underlying connection if any
}

type FakeTCPConn struct {
	tcp         *FakeTCP
	established chan bool
	buffer      gopacket.SerializeBuffer
	options     gopacket.SerializeOptions
	rnd         *mrand.Rand

	dst_ip      net.IP
	dst_port    int

	ip4_id      uint16
	seq         uint32
	ack         uint32

	ack_time    time.Time
}

type packet_t struct {
	from net.Addr
	data []byte
}

func newTCPConn(tcp *FakeTCP, dstIP net.IP, dstPort int) *FakeTCPConn {
	conn := new(FakeTCPConn)
	conn.tcp = tcp
	conn.established = make(chan bool, 1)
	conn.buffer = gopacket.NewSerializeBuffer()
	conn.options = gopacket.SerializeOptions{
		ComputeChecksums:true,
		FixLengths:true,
	}

	s := mrand.NewSource(time.Now().UnixNano())
	conn.rnd = mrand.New(s)

	conn.dst_ip = dstIP
	conn.dst_port = dstPort
	binary.Read(rand.Reader, binary.BigEndian, &conn.ip4_id)
	binary.Read(rand.Reader, binary.BigEndian, &conn.seq)
	conn.ack = 0
	conn.ack_time = time.Now()

	return conn
}

func (conn *FakeTCPConn) String() string {
	return fmt.Sprintf("%s:%d -> %s:%d", conn.tcp.local_ip, conn.tcp.local_port,
		conn.dst_ip, conn.dst_port)
}

func (conn *FakeTCPConn) WritePacket(flags int, data []byte) error {

	window := uint16(conn.rnd.Intn(16412 - 128) + 128)

	ethernetLayer := &layers.Ethernet{
		SrcMAC: conn.tcp.local_mac,
		DstMAC: conn.tcp.gateway_mac,
		EthernetType: layers.EthernetTypeIPv4,
	}

	ipLayer := &layers.IPv4{
		Version:4,
		Protocol:layers.IPProtocolTCP,
		TTL:64,
		Flags: layers.IPv4DontFragment,
		SrcIP: conn.tcp.local_ip,
		DstIP: conn.dst_ip,
		Id:conn.ip4_id,
	}
	tcpLayer := &layers.TCP{
		SrcPort: layers.TCPPort(conn.tcp.local_port),
		DstPort: layers.TCPPort(conn.dst_port),
		Seq:     conn.seq,
		Ack:     conn.ack,
		SYN:     flags & syn > 0,
		ACK:     flags & ack > 0,
		PSH:     flags & psh > 0,
		Window:  window,
	}
	tcpLayer.SetNetworkLayerForChecksum(ipLayer)

	err := gopacket.SerializeLayers(conn.buffer, conn.options,
		ethernetLayer,
		ipLayer,
		tcpLayer,
		gopacket.Payload(data),
	)
	if err != nil {
		return err
	}

	outgoingPacket := conn.buffer.Bytes()

	err = conn.tcp.send_handle.WritePacketData(outgoingPacket)
	if err != nil {
		return err
	}

	conn.ip4_id++
	if flags & psh > 0 {
		//if time.Now().After(conn.ack_time) {
		//	conn.WritePacket(ack, nil)
		//	conn.ack_time = time.Now().Add(time.Duration(time.Millisecond * 1000))
		//}
		conn.seq += uint32(len(data))
	}

	return nil
}

func (conn *FakeTCPConn) Connect() error {

	conn.tcp.local_port = uint16(conn.rnd.Intn(64 * 1024 - 1 - 10000) + 10000) // 10000 - 65536

	filter := fmt.Sprintf("tcp and dst port %d", conn.tcp.local_port)
	if err := conn.tcp.recv_handle.SetBPFFilter(filter); err != nil {
		panic(err)
	}

	log.Printf("connecting to: %s", conn)
	for i := 0; i < 8; i++ {
		conn.WritePacket(syn, nil) //syn
		select {
		case <-conn.established:
			conn.WritePacket(ack, nil) //ack
			return nil

		case <-time.After(3000 * time.Millisecond):
			log.Printf("syn timeout: %d", i)
		}
	}
	return errors.New("connect timeout")
}

func (tcp *FakeTCP)handlePacket(packet gopacket.Packet) {

	if !tcp.initialized {
		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer != nil {
			ip, _ := ipLayer.(*layers.IPv4)
			if ip.DstIP.Equal(tcp.bing_ip) {

				if ethernetLayer := packet.Layer(layers.LayerTypeEthernet); ethernetLayer != nil {
					ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)

					tcp.local_mac = ethernetPacket.SrcMAC
					tcp.local_ip = ip.SrcIP
					tcp.gateway_mac = ethernetPacket.DstMAC
					tcp.ch_initialized <- true
				}
			}
		}
	} else {
		select {
		case tcp.ch_packets <- packet:
		default:
			log.Println("ch_packets is full.")
		}
	}
}

func (tcp *FakeTCP)processPackets() {

	for {
		packet := <-tcp.ch_packets
		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer != nil {

			tcpLayer := packet.Layer(layers.LayerTypeTCP)
			if tcpLayer != nil {
				ipPacket, _ := ipLayer.(*layers.IPv4)
				tcpPacket, _ := tcpLayer.(*layers.TCP)

				if uint16(tcpPacket.DstPort) == tcp.local_port {
					//inbound
					addr := fmt.Sprintf("%s:%d", ipPacket.SrcIP, tcpPacket.SrcPort)

					if tcpPacket.PSH && tcpPacket.ACK {

						conn, ok := tcp.connections[addr]
						if ok {
							conn.ack = tcpPacket.Seq
						}

						from := net.UDPAddr{IP:ipPacket.SrcIP, Port: int(tcpPacket.SrcPort)}
						data := tcpPacket.LayerPayload()
						select {
						case tcp.ch_payload_packets <- packet_t{&from, data}:
						default:
							log.Println("ch_payload_packets is full.")
						}

					} else {
						if (tcp.is_server) {
							conn, ok := tcp.connections[addr]
							if tcpPacket.SYN {
								if !ok {
									conn = newTCPConn(tcp, ipPacket.SrcIP, int(tcpPacket.SrcPort))
									conn.tcp.connections[addr] = conn
									log.Printf("add tcp connection: %s", conn)
								}
								conn.ack = tcpPacket.Seq + 1
								conn.WritePacket(syn | ack, nil) // syn/ack
							} else if tcpPacket.ACK {
								if ok && tcpPacket.Ack == conn.seq + 1 {
									conn.seq += 1
									log.Printf("tcp connection established: %s", conn)
								}
							}
						} else {

							if tcpPacket.SYN && tcpPacket.ACK {
								conn, ok := tcp.connections[addr]
								if ok && tcpPacket.Ack == conn.seq + 1 {
									conn.seq += 1
									conn.ack = tcpPacket.Seq + 1
									conn.WritePacket(ack, nil) // ack
									conn.established <- true
									log.Printf("tcp connection established: %s", conn)
								}
							}
						}
					}

				}
			}
		}
	}
}

func executeCmd(format string, args ...interface{}) error {
	cmd := fmt.Sprintf(format, args...)
	parts := strings.Fields(cmd)
	head := parts[0]
	parts = parts[1:]

	err := exec.Command(head, parts...).Run()
	return err
}

func isAdmin() bool {

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return os.Getuid() == 0
	} else if runtime.GOOS == "windows" {
		file, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		defer file.Close()
		if err == nil {
			return true
		}
	} else {
		//TODO
		panic("not supported os:" + runtime.GOOS)
	}

	return false
}

func getIptablesRows(chain, comment string) ([]int, error) {

	rows := []int{}
	out, err := exec.Command("iptables", "-L", chain, "-n", "--line-number").Output()
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, comment) {
			index := strings.Index(line, "   ")
			if index > 0 {
				n, err := strconv.Atoi(line[:index])
				if err == nil && n >= 0 {
					rows = append(rows, n)
				}
			}
		}
	}
	log.Println(rows)
	sort.Sort(sort.Reverse(sort.IntSlice(rows)))
	log.Println(rows)
	return rows, nil
}

func (tcp *FakeTCP) Initialize(laddr string, device string, server bool) error {

	device = strings.TrimSpace(device)
	tcp.ch_initialized = make(chan bool)
	tcp.connections = make(map[string]*FakeTCPConn)
	tcp.ch_payload_packets = make(chan packet_t, 1024 * 10)
	tcp.ch_packets = make(chan gopacket.Packet, 1024 * 10)
	tcp.is_server = server

	if !isAdmin() {
		log.Fatal("Permission denied, are you root?")
	}

	udpaddr, err := net.ResolveUDPAddr("udp", laddr)
	if err != nil {
		panic(err)
	}

	port := udpaddr.Port
	ip := udpaddr.IP

	//添加防火墙规则
	if runtime.GOOS == "windows" {

		if err := executeCmd("netsh advfirewall set allprofiles state on"); err != nil {
			panic(err)
		}

		if server {

			//clear rules
			executeCmd("netsh advfirewall firewall delete rule name=kcptun_server")

			if err := executeCmd("netsh advfirewall firewall add rule name=kcptun_server protocol=TCP dir=in localport=%v  action=block", port); err != nil {
				panic(err)
			}
			if err := executeCmd("netsh advfirewall firewall add rule name=kcptun_server protocol=TCP dir=out localport=%v  action=block", port); err != nil {
				panic(err)
			}
		} else {

			//clear rules
			executeCmd("netsh advfirewall firewall delete rule name=kcptun_client")

			if err := executeCmd("netsh advfirewall firewall add rule name=kcptun_client protocol=TCP dir=in remoteport=%v remoteip=%v action=block", port, ip); err != nil {
				panic(err)
			}
			if err := executeCmd("netsh advfirewall firewall add rule name=kcptun_client protocol=TCP dir=out remoteport=%v remoteip=%v action=block", port, ip); err != nil {
				panic(err)
			}
		}

	} else if runtime.GOOS == "linux" {

		if server {
			//clear kcptun_server rules
			err := exec.Command("bash", "-c", "iptables -S | sed \"/kcptun_server/s/-A/iptables -D/e\"").Run()
			if err != nil {
				panic(err)
			}

			//drop input
			if err := executeCmd("iptables -I INPUT -p tcp --dport %d -j DROP -m comment --comment %s",
				port, "kcptun_server"); err != nil {
				panic(err)
			}

		} else {
			//clear kcptun_client rules
			err := exec.Command("bash", "-c", "iptables -S | sed \"/kcptun_client/s/-A/iptables -D/e\"").Run()
			if err != nil {
				panic(err)
			}

			//drop input
			if err := executeCmd("iptables -I INPUT -p tcp -s %s --sport %d -j DROP -m comment --comment %s",
				ip, port, "kcptun_client"); err != nil {
				panic(err)
			}
			//drop output
			if err := executeCmd("iptables -I OUTPUT -p tcp -d %s --dport %d -j DROP -m comment --comment %s",
				ip, port, "kcptun_client"); err != nil {
				panic(err)
			}
		}

	} else if runtime.GOOS == "darwin" {
		//Yosemite firewall: IPFW is gone, Moving to PF
		//see https://superuser.com/questions/905645/ipfw-not-available-on-mac-os-x-yosemite

		//enable pf
		exec.Command("pfctl -e")

		//TODO 下面操作会清空现有规则...
		if server {
			err := exec.Command("bash", "-c",
				fmt.Sprintf("echo 'block drop quick proto tcp from any to any port %d' | sudo pfctl -f -", port)).Run()
			if err != nil {
				panic(err)
			}

		} else {
			err := exec.Command("bash", "-c",
				fmt.Sprintf("echo 'block drop quick proto tcp from %s port %d to any' | sudo pfctl -f -", ip, port)).Run()
			if err != nil {
				panic(err)
			}
		}

	} else {
		panic("not supported os: " + runtime.GOOS)
	}


	//select network interface
	devs, err := pcap.FindAllDevs()
	if err != nil {
		panic(err)
	}
	var dev *pcap.Interface = nil
	if device != "" {
		for _, d := range devs {
			if strings.EqualFold(device, d.Name) {
				dev = &d
				break
			}
		}
	}
	if dev == nil {

		if (device != "") {
			fmt.Printf("device: %s not found\n", device)
		}

		fmt.Println("available devices:")
		for i, dev := range devs {
			fmt.Printf("[%d] name: %s, desc: %s, address: %v\n", i + 1, dev.Name, dev.Description, dev.Addresses)
		}

		var line string
		fmt.Println("select interface to listen on:")
		fmt.Scanln(&line)
		i, err := strconv.Atoi(line)
		if err != nil {
			panic(err)
		}

		dev = &devs[i - 1]
	}
	log.Printf("sniffing on interface: %v\n", *dev)

	var recvHandle *pcap.Handle;
	var sendHanle *pcap.Handle;

	promiscuousMode := true
	if strings.Contains(strings.ToLower(dev.Description), "wireless") {
		promiscuousMode = false
		log.Println("promiscuousMode = false")
	}

	if sendHanle, err = pcap.OpenLive(dev.Name, 1600 * 10, promiscuousMode, time.Millisecond * 1); err != nil {
		panic(err)
	}
	if recvHandle, err = pcap.OpenLive(dev.Name, 1600 * 10, promiscuousMode, time.Millisecond * 1); err != nil {
		panic(err)
	}
	if err = recvHandle.SetBPFFilter("tcp and port 80"); err != nil {
		panic(err)
	} else {
		go func() {
			packetSource := gopacket.NewPacketSource(recvHandle, recvHandle.LinkType())
			for packet := range packetSource.Packets() {
				tcp.handlePacket(packet)
			}
		}()
		go tcp.processPackets()
	}
	tcp.send_handle = sendHanle
	tcp.recv_handle = recvHandle

	tcpaddr, err := net.ResolveTCPAddr("tcp", "www.bing.com:80")
	if err != nil {
		panic(err)
	} else {
		tcp.bing_ip = tcpaddr.IP
	}

	_, err = net.DialTCP("tcp", nil, tcpaddr)
	if err != nil {
		panic(err)
	}
	tcp.initialized = <-tcp.ch_initialized

	if server {
		tcp.local_port = uint16(port)
		filter := fmt.Sprintf("tcp and dst port %d", tcp.local_port)
		recvHandle.SetBPFFilter(filter)
	} else {
		//客服端在Connect设置Filter
	}

	//go func() {
	//	for {
	//		select {
	//		case <-time.After(3000 * time.Millisecond):
	//			stats, _ := recvHandle.Stats()
	//			log.Printf("chan: %v-%v, recv: %v, drop: %v, ifDrop: %v\n",
	//				len(tcp.ch_packets), len(tcp.ch_payload_packets),
	//				stats.PacketsReceived, stats.PacketsDropped, stats.PacketsIfDropped)
	//		}
	//	}
	//}()

	log.Printf("local: %v [%v], gateway: %v\n", tcp.local_mac, tcp.local_ip, tcp.gateway_mac)

	return nil
}

func (tcp *FakeTCP) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	packet := <-tcp.ch_payload_packets
	addr = packet.from
	n = copy(b, packet.data)
	return n, addr, nil
}

func (tcp *FakeTCP) WriteTo(b []byte, addr net.Addr) (int, error) {
	session, ok := tcp.connections[addr.String()]
	if ok {
		if err := session.WritePacket(psh | ack, b); err == nil {
			return len(b), nil
		} else {
			log.Printf("SendData err: %v", err)
			return 0, err
		}
	} else {
		return 0, errors.New("broken pipe")
	}
}

func (tcp *FakeTCP) LocalAddr() net.Addr {
	addr := new(net.UDPAddr)
	addr.IP = tcp.local_ip
	addr.Port = int(tcp.local_port)
	return addr
}

func (tcp *FakeTCP)Read(b []byte) (n int, err error) {
	panic("Read Not Implemented")
}

func (tcp *FakeTCP)Write(b []byte) (n int, err error) {
	panic("Write Not Implemented")
}

func (tcp *FakeTCP) Close() error {

	if (tcp.is_server) {
		//TODO 服务端UDPSession关闭后需要清理FakeTCPConn
		panic("Close Not Implemented")
	} else {

		if err := tcp.recv_handle.SetBPFFilter("tcp and src 127.0.0.1"); err != nil {
			panic(err)
		}

		//clear ch_payload_packets?
		for {
			select {
			case <-tcp.ch_payload_packets:
			default:
				tcp.connections = make(map[string]*FakeTCPConn)
				log.Println("tcp connection closed")
				return nil
			}
		}

	}
}
