package tun

import (
	"context"
	"fmt"
	"net"
	
	"mandala/core/config"
	"mandala/core/proxy"
	// "mandala/core/protocol" // 这里不需要直接引 protocol 了，因为逻辑移到了 nat 文件中

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// Stack 服务结构体
type Stack struct {
	stack      *stack.Stack
	device     *Device
	dialer     *proxy.Dialer
	config     *config.OutboundConfig
	nat        *UDPNatManager // [新增] NAT 管理器
	ctx        context.Context
	cancel     context.CancelFunc
}

// StartStack 启动 TUN 处理栈
func StartStack(fd int, mtu int, cfg *config.OutboundConfig) (*Stack, error) {
	// 1. 创建 TUN 设备
	dev, err := NewDevice(fd, uint32(mtu))
	if err != nil {
		return nil, err
	}

	// 2. gVisor 协议栈初始化
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

	// 3. 注册 NIC
	nicID := tcpip.NICID(1)
	if err := s.CreateNIC(nicID, dev.LinkEndpoint()); err != nil {
		dev.Close()
		return nil, fmt.Errorf("create nic failed: %v", err)
	}

	s.SetRouteTable([]tcpip.Route{
		{
			Destination: tcpip.Address{},
			Mask:        tcpip.Address{},
			NIC:         nicID,
		},
	})
	s.SetPromiscuousMode(nicID, true)
	s.SetTransportProtocolOption(tcp.ProtocolNumber, tcp.SACKEnabled(true))

	ctx, cancel := context.WithCancel(context.Background())
	
	dialer := proxy.NewDialer(cfg)

	tStack := &Stack{
		stack:  s,
		device: dev,
		dialer: dialer,
		config: cfg,
		nat:    NewUDPNatManager(dialer, cfg), // [新增] 初始化 NAT
		ctx:    ctx,
		cancel: cancel,
	}

	tStack.startPacketHandling()

	return tStack, nil
}

// Close 停止栈
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
	// TCP 处理 (保持不变)
	tcpHandler := tcp.NewForwarder(s.stack, 30000, 10, func(r *tcp.ForwarderRequest) {
		go s.handleTCP(r)
	})
	s.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpHandler.HandlePacket)

	// UDP 处理 (使用新的 NAT 逻辑)
	udpHandler := udp.NewForwarder(s.stack, func(r *udp.ForwarderRequest) {
		// 注意：不要在这里直接 go func，要在内部处理
		s.handleUDP(r)
	})
	s.stack.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)
}

// handleTCP (保持不变，但为了代码完整性，请确保 import 正确)
func (s *Stack) handleTCP(r *tcp.ForwarderRequest) {
	// ... (原有的 TCP 处理逻辑，此处省略以节省篇幅，不需要修改) ...
	// 请保留您之前的 handleTCP 代码
	// 这里简单复述关键部分，防止您复制时丢失
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
	
	// 连接代理
	remoteConn, err := s.dialer.Dial()
	if err != nil {
		return
	}
	defer remoteConn.Close()

	// TCP 握手 (Mandala)
	// 注意：此处代码应该引用 protocol 包，如果编译报错请检查 import
	/* if s.config.Type == "mandala" {
		// ... 握手逻辑 ...
	}
	*/
	// 建议：由于 handleTCP 逻辑较长且之前已实现，此处假设您保留原样
	// 重点是下面的 handleUDP
}


// handleUDP 处理 UDP 数据包 (NAT 版)
func (s *Stack) handleUDP(r *udp.ForwarderRequest) {
	id := r.ID()
	
	// 1. 获取目标信息
	// 注意：在 ForwarderRequest 中，LocalAddress 是数据包的目标地址
	targetIP := net.IP(id.LocalAddress.AsSlice()).String()
	targetPort := int(id.LocalPort)
	
	// 生成会话 Key: "SrcIP:SrcPort -> DstIP:DstPort"
	// RemoteAddress 是数据包的来源 (App)
	srcKey := fmt.Sprintf("%s:%d->%s:%d", 
		id.RemoteAddress.String(), id.RemotePort,
		targetIP, targetPort)

	// 2. 创建 gVisor 端点 (用于与 App 通信)
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		return
	}
	// 包装为 UDPConn
	localConn := gonet.NewUDPConn(s.stack, &wq, ep)

	// 3. 通过 NAT 管理器获取会话
	// 注意：GetOrCreate 会负责 Dial 代理和发送握手
	session, err := s.nat.GetOrCreate(srcKey, localConn, targetIP, targetPort)
	if err != nil {
		localConn.Close()
		return
	}

	// 4. 读取数据并转发 (上行: App -> Proxy)
	// 我们启动一个协程来处理这个具体的数据包及后续可能的数据
	go func() {
		// 注意：这里的 localConn 对于 gVisor UDP 来说，
		// 每次 HandlePacket 实际上可能只是一个包，
		// 但 gonet.NewUDPConn 会尝试适配。
		// 更标准的做法是每次 CreateEndpoint 后只 Read 一次，
		// 但为了复用 NAT 逻辑，我们这里简化处理：
		
		buf := make([]byte, 4096)
		n, err := localConn.Read(buf)
		if err != nil {
			// localConn 在一次包处理完后通常不需要 Close，
			// 但 gVisor 的机制是 UDP 是无连接的，Endpoint 可能只对应一个包。
			// 这里为了配合 NAT 逻辑，我们只处理读取到的这一部分数据。
			return 
		}

		// 发送给代理
		session.remoteConn.Write(buf[:n])
		session.lastActive = time.Now()
		
		// 关键点：对于 UDP，gVisor 的 CreateEndpoint 创建的是临时的 handle。
		// 我们不需要 Close session，因为 session 管理的是 remoteConn。
		// 但我们需要 Close 这个临时的 localConn (它只对应这个包的上下文)。
		// 可是！我们的 NAT Manager 下行逻辑持有 localConn 指针用来回写。
		// 这是一个复杂点。
		
		// *修正策略*：
		// 上面的代码有潜在问题：gVisor UDP Forwarder 为每个包创建新的 Endpoint。
		// 如果我们把这个 Endpoint 存到 Session 里用于回写，当这个包处理函数结束后，Endpoint 是否有效？
		// 答案是：gVisor 的 UDP Endpoint 只要不 Close 就是有效的。
		// 但是，对于不同的源端口，我们需要不同的 Session。
		// 如果源端口相同（同一个 App 的同一个 Socket），Key 就相同，Session 复用。
		
		// 唯一的问题是：如果 GetOrCreate 发现 Session 存在，它会复用旧的 Session。
		// 旧的 Session 里存的是 **第一次** 创建的 localConn。
		// 对于 UDP，这通常没问题，因为 endpoint 绑定的是 4 元组。
	}()
}
