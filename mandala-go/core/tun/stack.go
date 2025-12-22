package tun

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
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

func init() {
	log.SetPrefix("GoLog: ")
}

type Stack struct {
	stack     *stack.Stack
	device    *Device
	dialer    *proxy.Dialer
	config    *config.OutboundConfig
	nat       *UDPNatManager
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
}

func StartStack(fd int, mtu int, cfg *config.OutboundConfig) (*Stack, error) {
	log.Printf("[Stack] 启动中 (FD: %d, MTU: %d, Type: %s)", fd, mtu, cfg.Type)

	dev, err := NewDevice(fd, uint32(mtu))
	if err != nil {
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

	s.SetForwardingDefaultAndAllNICs(ipv4.ProtocolNumber, true)
	s.SetForwardingDefaultAndAllNICs(ipv6.ProtocolNumber, true)

	nicID := tcpip.NICID(1)
	if err := s.CreateNIC(nicID, dev.LinkEndpoint()); err != nil {
		dev.Close()
		return nil, fmt.Errorf("创建网卡失败: %v", err)
	}

	s.SetPromiscuousMode(nicID, true)
	s.SetSpoofing(nicID, true)

	s.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
	})

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

func (s *Stack) startPacketHandling() {
	// 使用系统默认窗口大小，通常比手动指定更稳定
	// 如果需要手动指定，建议配合 TCP 缓冲区自动调整逻辑
	// 这里改回 0，让 gVisor 自行管理，避免 1<<20 导致部分环境内存压力
	rcvWnd := 0 
	maxInFlight := 2048

	tcpHandler := tcp.NewForwarder(s.stack, rcvWnd, maxInFlight, func(r *tcp.ForwarderRequest) {
		go s.handleTCP(r)
	})
	s.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpHandler.HandlePacket)

	udpHandler := udp.NewForwarder(s.stack, func(r *udp.ForwarderRequest) {
		s.handleUDP(r)
	})
	s.stack.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)
}

func (s *Stack) handleTCP(r *tcp.ForwarderRequest) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[TCP] Panic: %v", err)
		}
	}()

	id := r.ID()

	remoteConn, dialErr := s.dialer.Dial()
	if dialErr != nil {
		r.Complete(true)
		return
	}
	// [关键修复] defer Close 必须配合后面的 wg.Wait() 才能安全使用
	// 否则主线程退出会立即切断连接
	defer remoteConn.Close()

	var payload []byte
	var hErr error
	targetHost := id.LocalAddress.String()
	targetPort := int(id.LocalPort)
	
	// 判断是否为 WebSocket 模式，用于后续逻辑判断
	isWebSocket := s.config.Transport != nil && s.config.Transport.Type == "ws"

	switch strings.ToLower(s.config.Type) {
	case "mandala":
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		payload, hErr = client.BuildHandshakePayload(targetHost, targetPort)
	case "trojan":
		payload, hErr = protocol.BuildTrojanPayload(s.config.Password, targetHost, targetPort)
	case "vless":
		payload, hErr = protocol.BuildVlessPayload(s.config.UUID, targetHost, targetPort)
	case "shadowsocks":
		payload, hErr = protocol.BuildShadowsocksPayload(targetHost, targetPort)
	case "socks", "socks5":
		hErr = protocol.HandshakeSocks5(remoteConn, s.config.Username, s.config.Password, targetHost, targetPort)
	}

	if hErr != nil {
		r.Complete(true)
		return
	}

	if len(payload) > 0 {
		if _, err := remoteConn.Write(payload); err != nil {
			r.Complete(true)
			return
		}
	}

	// 如果是 VLESS 且不是 WS 模式，才包装连接
	// 如果是 WS 模式，VLESS 头部通常包含在 WS 握手或第一帧中，视具体服务端实现而定
	// 你之前提到 VLESS 正常，保持原有逻辑
	if strings.ToLower(s.config.Type) == "vless" && !isWebSocket {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		r.Complete(true)
		return
	}
	r.Complete(false)

	localConn := gonet.NewTCPConn(&wq, ep)
	defer localConn.Close()

	// [关键修复] 使用 WaitGroup 确保双向流都结束
	var wg sync.WaitGroup
	wg.Add(1)

	// 下载流: Remote -> App
	// 这部分数据是用户 "接收" 的网页内容
	go func() {
		defer wg.Done()
		io.Copy(localConn, remoteConn)
		// 告诉 App 数据传完了
		localConn.CloseWrite()
	}()

	// 上传流: App -> Remote
	// 这部分是用户发送的请求 (如 HTTP GET)
	io.Copy(remoteConn, localConn)

	// 上传完成后，尝试通知服务器 "我发完了" (TCP Half-Close)
	// 这样服务器知道请求结束，但连接依然保持打开，用于发送响应
	if cw, ok := remoteConn.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}

	// [最关键的一行]
	// 等待下载流 Goroutine 结束。
	// 如果不加这行，主线程会立即执行 defer remoteConn.Close()，
	// 导致服务器还没发完的数据被强行切断 -> 造成断流。
	wg.Wait()
}

func (s *Stack) handleUDP(r *udp.ForwarderRequest) {
	id := r.ID()
	targetPort := int(id.LocalPort)

	if targetPort == 53 {
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			return
		}
		localConn := gonet.NewUDPConn(s.stack, &wq, ep)
		go s.handleRemoteDNS(localConn)
		return
	}

	targetIP := net.IP(id.LocalAddress.AsSlice()).String()
	srcKey := fmt.Sprintf("%s:%d->%s:%d", id.RemoteAddress.String(), id.RemotePort, targetIP, targetPort)

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
			localConn.SetDeadline(time.Now().Add(60 * time.Second))
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

func (s *Stack) handleRemoteDNS(conn *gonet.UDPConn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	proxyConn, err := s.dialer.Dial()
	if err != nil {
		return
	}
	defer proxyConn.Close()

	var payload []byte
	isVless := false
	isWebSocket := s.config.Transport != nil && s.config.Transport.Type == "ws"

	switch strings.ToLower(s.config.Type) {
	case "mandala":
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		payload, _ = client.BuildHandshakePayload("8.8.8.8", 53)
	case "trojan":
		payload, _ = protocol.BuildTrojanPayload(s.config.Password, "8.8.8.8", 53)
	case "vless":
		payload, _ = protocol.BuildVlessPayload(s.config.UUID, "8.8.8.8", 53)
		isVless = true
	case "shadowsocks":
		payload, _ = protocol.BuildShadowsocksPayload("8.8.8.8", 53)
	case "socks", "socks5":
		if err := protocol.HandshakeSocks5(proxyConn, s.config.Username, s.config.Password, "8.8.8.8", 53); err != nil {
			return
		}
	}

	if len(payload) > 0 {
		if _, err := proxyConn.Write(payload); err != nil {
			return
		}
	}

	if isVless && !isWebSocket {
		proxyConn = protocol.NewVlessConn(proxyConn)
	}

	reqData := make([]byte, 2+n)
	reqData[0] = byte(n >> 8)
	reqData[1] = byte(n)
	copy(reqData[2:], buf[:n])

	if _, err := proxyConn.Write(reqData); err != nil {
		return
	}

	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(proxyConn, lenBuf); err != nil {
		return
	}
	respLen := int(lenBuf[0])<<8 | int(lenBuf[1])

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(proxyConn, respBuf); err != nil {
		return
	}

	conn.Write(respBuf)
	log.Printf("[DNS] 解析完成")
}

func (s *Stack) Close() {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		time.Sleep(100 * time.Millisecond)
		if s.device != nil {
			s.device.Close()
		}
		if s.stack != nil {
			s.stack.Close()
		}
	})
}
