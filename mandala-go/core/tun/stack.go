package tun

import (
	"context"
	"fmt"
	"io"
	"log" // [关键] 必须使用 log 包，不能只用 fmt
	"net"
	"time"

	"mandala/core/config"
	"mandala/core/protocol"
	"mandala/core/proxy"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// 初始化日志前缀，方便在 Logcat 中搜索 "GoLog"
func init() {
	log.SetPrefix("GoLog: ")
	log.SetFlags(log.Ltime | log.Lshortfile)
}

type Stack struct {
	stack  *stack.Stack
	device *Device
	dialer *proxy.Dialer
	config *config.OutboundConfig
	nat    *UDPNatManager
	ctx    context.Context
	cancel context.CancelFunc
}

func StartStack(fd int, mtu int, cfg *config.OutboundConfig) (*Stack, error) {
	// [调试日志] 启动入口
	log.Printf("=== Go Core Starting (FD: %d, MTU: %d) ===", fd, mtu)

	dev, err := NewDevice(fd, uint32(mtu))
	if err != nil {
		log.Printf("Error creating device: %v", err)
		return nil, err
	}

	s := stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			ipv6.NewProtocol,
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			udp.NewProtocol,
		},
	})

	nicID := tcpip.NICID(1)
	if err := s.CreateNIC(nicID, dev.LinkEndpoint()); err != nil {
		dev.Close()
		log.Printf("Error creating NIC: %v", err)
		return nil, fmt.Errorf("create nic failed: %v", err)
	}

	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet, 
			NIC:         nicID,
		},
	})

	s.SetPromiscuousMode(nicID, true)

	ctx, cancel := context.WithCancel(context.Background())
	dialer := proxy.NewDialer(cfg)

	tStack := &Stack{
		stack:  s,
		device: dev,
		dialer: dialer,
		config: cfg,
		nat:    NewUDPNatManager(dialer, cfg),
		ctx:    ctx,
		cancel: cancel,
	}

	tStack.startPacketHandling()
	
	log.Println("=== Go Core Initialized Successfully ===")
	return tStack, nil
}

func (s *Stack) Close() {
	log.Println("Go Core Closing...")
	s.cancel()
	if s.device != nil {
		s.device.Close()
	}
	if s.stack != nil {
		s.stack.Close()
	}
}

func (s *Stack) startPacketHandling() {
	tcpHandler := tcp.NewForwarder(s.stack, 30000, 10, func(r *tcp.ForwarderRequest) {
		go s.handleTCP(r)
	})
	s.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpHandler.HandlePacket)

	udpHandler := udp.NewForwarder(s.stack, func(r *udp.ForwarderRequest) {
		s.handleUDP(r)
	})
	s.stack.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)
}

func (s *Stack) handleTCP(r *tcp.ForwarderRequest) {
	// 捕获 panic 防止崩坏
	defer func() {
		if r := recover(); r != nil {
			log.Printf("TCP Panic: %v", r)
		}
	}()

	id := r.ID()
	// [调试日志] 打印所有 TCP 请求的目标
	// 如果你看到大量指向你自己代理服务器 IP 的请求，说明还是死循环
	log.Printf("TCP Connect: %s:%d", id.LocalAddress, id.LocalPort)

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		log.Printf("TCP CreateEndpoint error: %v", err)
		r.Complete(true)
		return
	}
	r.Complete(false)

	localConn := gonet.NewTCPConn(&wq, ep)
	defer localConn.Close()

	remoteConn, dialErr := s.dialer.Dial()
	if dialErr != nil {
		log.Printf("Proxy Dial Failed: %v", dialErr)
		return
	}
	defer remoteConn.Close()

	// ... 握手逻辑 (保持不变) ...
	var handshakeErr error
	var handshakePayload []byte

	switch s.config.Type {
	case "mandala":
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		handshakePayload, handshakeErr = client.BuildHandshakePayload(id.LocalAddress.String(), int(id.LocalPort))
	}

	if handshakeErr != nil {
		log.Printf("Handshake Error: %v", handshakeErr)
		return
	}

	if len(handshakePayload) > 0 {
		if _, wErr := remoteConn.Write(handshakePayload); wErr != nil {
			return
		}
	}

	go io.Copy(remoteConn, localConn)
	io.Copy(localConn, remoteConn)
}

func (s *Stack) handleUDP(r *udp.ForwarderRequest) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("UDP Panic: %v", r)
		}
	}()

	id := r.ID()
	targetPort := int(id.LocalPort)

	// [调试日志] 确认 DNS 请求是否到达
	if targetPort == 53 {
		log.Println("UDP/53 DNS Request Intercepted! Handling locally...")
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			log.Printf("DNS CreateEndpoint error: %v", err)
			return
		}
		localConn := gonet.NewUDPConn(s.stack, &wq, ep)
		go s.handleLocalDNS(localConn)
		return
	}

	// ... 普通 UDP NAT 逻辑 ...
	targetIP := net.IP(id.LocalAddress.AsSlice()).String()
	srcKey := fmt.Sprintf("%s:%d->%s:%d",
		id.RemoteAddress.String(), id.RemotePort,
		targetIP, targetPort)

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		return
	}

	localConn := gonet.NewUDPConn(s.stack, &wq, ep)
	session, natErr := s.nat.GetOrCreate(srcKey, localConn, targetIP, targetPort)
	if natErr != nil {
		localConn.Close()
		return
	}

	go func() {
		defer localConn.Close()
		buf := make([]byte, 4096)
		for {
			n, rErr := localConn.Read(buf)
			if rErr != nil {
				return
			}
			if _, wErr := session.RemoteConn.Write(buf[:n]); wErr != nil {
				return
			}
		}
	}()
}

// 使用 TCP 协议请求国内 DNS (阿里 DNS)
func (s *Stack) handleLocalDNS(conn *gonet.UDPConn) {
	defer conn.Close()
	
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("DNS Read internal error: %v", err)
		return
	}

	// 阿里 DNS
	realDNS := "223.5.5.5:53"
	
	log.Printf("Dialing DNS (TCP) to %s...", realDNS)
	tcpConn, err := net.DialTimeout("tcp", realDNS, 3*time.Second)
	if err != nil {
		log.Printf("DNS Dial TCP failed: %v", err)
		return
	}
	defer tcpConn.Close()

	// 构造 DNS over TCP 包 (前2字节为长度)
	reqData := make([]byte, 2+n)
	reqData[0] = byte(n >> 8)
	reqData[1] = byte(n)
	copy(reqData[2:], buf[:n])

	if _, err := tcpConn.Write(reqData); err != nil {
		log.Printf("DNS Write failed: %v", err)
		return
	}

	// 读取长度
	tcpConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(tcpConn, lenBuf); err != nil {
		log.Printf("DNS Read Len failed: %v", err)
		return
	}
	respLen := int(lenBuf[0])<<8 | int(lenBuf[1])

	// 读取 Payload
	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(tcpConn, respBuf); err != nil {
		log.Printf("DNS Read Payload failed: %v", err)
		return
	}

	log.Printf("DNS Resolved Successfully! Length: %d", respLen)
	conn.Write(respBuf)
}
