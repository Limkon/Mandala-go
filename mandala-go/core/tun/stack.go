// 文件路径: mandala-go/core/tun/stack.go

package tun

import (
	"context"
	"fmt"
	"io"
	"net"
	"time" // [必须引入]

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
	fmt.Println("DEBUG: Build Version 2025-Fixed-DNS-CN")

	dev, err := NewDevice(fd, uint32(mtu))
	if err != nil {
		return nil, err
	}

	// 1. 初始化协议栈
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
		return nil, fmt.Errorf("create nic failed: %v", err)
	}

	// 2. 路由表设置
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
	return tStack, nil
}

func (s *Stack) Close() {
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
	id := r.ID()
	targetIP := net.IP(id.LocalAddress.AsSlice())
	targetPort := id.LocalPort

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		r.Complete(true)
		return
	}
	r.Complete(false)

	localConn := gonet.NewTCPConn(&wq, ep)
	defer localConn.Close()

	remoteConn, dialErr := s.dialer.Dial()
	if dialErr != nil {
		return
	}
	defer remoteConn.Close()

	var handshakeErr error
	var handshakePayload []byte

	switch s.config.Type {
	case "mandala":
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		handshakePayload, handshakeErr = client.BuildHandshakePayload(targetIP.String(), int(targetPort))
	default:
	}

	if handshakeErr != nil {
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
	id := r.ID()
	targetPort := int(id.LocalPort)

	// [关键修复] 拦截 DNS 请求 (Port 53)
	// 直接在本地处理 DNS，绕过代理服务器，解决 UDP 转发不兼容导致的断网问题
	if targetPort == 53 {
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			return
		}
		// 创建本地 VPN 内的 UDP 连接
		localConn := gonet.NewUDPConn(s.stack, &wq, ep)
		
		// 启动协程处理 DNS 转发
		go s.handleLocalDNS(localConn)
		return
	}

	targetIP := net.IP(id.LocalAddress.AsSlice()).String()
	srcKey := fmt.Sprintf("%s:%d->%s:%d",
		id.RemoteAddress.String(), id.RemotePort,
		targetIP, targetPort)

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		return
	}

	// [重要] 3个参数，适配您锁定的旧版 gVisor
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

// [新增] 本地 DNS 转发逻辑 (适配国内网络)
// 这里的流量会因为 Android 侧的 addDisallowedApplication 而直接走物理 Wi-Fi/5G
func (s *Stack) handleLocalDNS(conn *gonet.UDPConn) {
	defer conn.Close()
	
	// 设置内部读取超时，防止协程泄露
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	buf := make([]byte, 1500) // 标准 MTU 大小
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	// [修改] 使用国内 DNS (阿里 DNS) 以适应大陆网络
	// 避免使用 8.8.8.8 被墙导致的超时
	realDNS := "223.5.5.5:53"
	
	// 设置外部连接超时
	dnsConn, err := net.DialTimeout("udp", realDNS, 2*time.Second)
	if err != nil {
		fmt.Printf("[DNS] Local dial to %s failed: %v\n", realDNS, err)
		return
	}
	defer dnsConn.Close()

	// 发送查询
	if _, err := dnsConn.Write(buf[:n]); err != nil {
		return
	}

	// 读取回复
	dnsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = dnsConn.Read(buf)
	if err != nil {
		fmt.Printf("[DNS] Read from real DNS failed: %v\n", err)
		return
	}

	// 将结果写回 VPN 内部
	conn.Write(buf[:n])
}
