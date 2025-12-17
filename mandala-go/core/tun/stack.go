package tun

import (
	"context"
	"fmt"
	"io"
	"net"

	"mandala/core/config"
	"mandala/core/protocol"
	"mandala/core/proxy"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
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
	dev, err := NewDevice(fd, uint32(mtu))
	if err != nil {
		return nil, err
	}

	// 初始化协议栈
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

	// [修复 1] 路由表适配新 API: 使用 Subnet 而不是 Mask
	// 创建一个 0.0.0.0/0 的子网
	subnet, _ := tcpip.NewSubnet(tcpip.AddrFrom4([4]byte{0, 0, 0, 0}), tcpip.MaskFromBytes([]byte{0, 0, 0, 0}))
	
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: subnet, // 新版使用 Destination (类型为 Subnet)
			NIC:         nicID,
		},
	})

	s.SetPromiscuousMode(nicID, true)

	// [修复 2] SACK 选项适配
	// 新版通常默认开启，或者使用具体的 Option 结构
	// 这里使用通用设置，如果 SetTransportProtocolOption 报错，可直接注释掉，因为现代 gVisor 默认已优化
	s.SetTransportProtocolOption(tcp.ProtocolNumber, tcp.SACKEnabled(true))

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
	// TCP
	tcpHandler := tcp.NewForwarder(s.stack, 30000, 10, func(r *tcp.ForwarderRequest) {
		go s.handleTCP(r)
	})
	s.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpHandler.HandlePacket)

	// UDP
	udpHandler := udp.NewForwarder(s.stack, func(r *udp.ForwarderRequest) {
		s.handleUDP(r)
	})
	s.stack.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)
}

func (s *Stack) handleTCP(r *tcp.ForwarderRequest) {
	id := r.ID()
	// 注意：新版 gVisor Address 可能需要 .AsSlice() 或直接使用
	targetIP := net.IP(id.LocalAddress.AsSlice())
	targetPort := id.LocalPort
	
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		r.Complete(true)
		return
	}
	r.Complete(false)

	// 将 Endpoint 转为 net.Conn
	localConn := gonet.NewTCPConn(&wq, ep)
	defer localConn.Close()
	
	fmt.Printf("[TCP] Connect to %s:%d\n", targetIP, targetPort)

	remoteConn, err := s.dialer.Dial()
	if err != nil {
		return
	}
	defer remoteConn.Close()

	if s.config.Type == "mandala" {
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		payload, err := client.BuildHandshakePayload(targetIP.String(), int(targetPort))
		if err != nil { return }
		if _, err := remoteConn.Write(payload); err != nil { return }
	}

	go io.Copy(remoteConn, localConn)
	io.Copy(localConn, remoteConn)
}

func (s *Stack) handleUDP(r *udp.ForwarderRequest) {
	id := r.ID()
	targetIP := net.IP(id.LocalAddress.AsSlice()).String()
	targetPort := int(id.LocalPort)
	
	srcKey := fmt.Sprintf("%s:%d->%s:%d", 
		id.RemoteAddress.String(), id.RemotePort,
		targetIP, targetPort)

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		return
	}
	
	// [修复 3] gonet.NewUDPConn 适配新 API
	// 新版不再需要传入 s.stack 参数
	localConn := gonet.NewUDPConn(&wq, ep)

	session, err := s.nat.GetOrCreate(srcKey, localConn, targetIP, targetPort)
	if err != nil {
		localConn.Close()
		return
	}

	go func() {
		defer localConn.Close()
		buf := make([]byte, 4096)
		n, err := localConn.Read(buf)
		if err != nil { return }
		session.RemoteConn.Write(buf[:n])
	}()
}
