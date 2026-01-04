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

	// [优化] DNS 连接复用
	dnsMu   sync.Mutex
	dnsConn net.Conn
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
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[TCP] Panic: %v", err)
		}
	}()

	id := r.ID()

	// 1. 拨号代理
	remoteConn, dialErr := s.dialer.Dial()
	if dialErr != nil {
		r.Complete(true)
		return
	}
	// [修复] 不再依赖简单的 defer remoteConn.Close()，而是通过 closeAll 统一管理

	// 2. 握手逻辑 (保持不变)
	var payload []byte
	var hErr error
	targetHost := id.LocalAddress.String()
	targetPort := int(id.LocalPort)
	isVless := false

	switch strings.ToLower(s.config.Type) {
	case "mandala":
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		payload, hErr = client.BuildHandshakePayload(targetHost, targetPort, s.config.Settings.Noise)
	case "trojan":
		payload, hErr = protocol.BuildTrojanPayload(s.config.Password, targetHost, targetPort)
	case "vless":
		payload, hErr = protocol.BuildVlessPayload(s.config.UUID, targetHost, targetPort)
		isVless = true
	case "shadowsocks":
		payload, hErr = protocol.BuildShadowsocksPayload(targetHost, targetPort)
	case "socks", "socks5":
		hErr = protocol.HandshakeSocks5(remoteConn, s.config.Username, s.config.Password, targetHost, targetPort)
	}

	if hErr != nil {
		remoteConn.Close()
		r.Complete(true)
		return
	}

	if len(payload) > 0 {
		if _, err := remoteConn.Write(payload); err != nil {
			remoteConn.Close()
			r.Complete(true)
			return
		}
	}

	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	// 3. 建立本地连接
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		remoteConn.Close()
		r.Complete(true)
		return
	}
	r.Complete(false)

	localConn := gonet.NewTCPConn(&wq, ep)

	// [修复] 健壮的双向转发关闭逻辑
	// 任何一端出错或结束，都确保两端 socket 被关闭
	closeAll := func() {
		localConn.Close()
		remoteConn.Close()
	}

	go func() {
		defer closeAll()
		io.Copy(localConn, remoteConn)
		// 远程读完(EOF)，通常意味着连接结束
	}()

	go func() {
		defer closeAll()
		io.Copy(remoteConn, localConn)
		// 本地读完(EOF)
	}()
}

func (s *Stack) handleUDP(r *udp.ForwarderRequest) {
	id := r.ID()
	targetPort := int(id.LocalPort)

	// [DNS处理] 拦截 53 端口
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

// [优化] handleRemoteDNS 复用连接
func (s *Stack) handleRemoteDNS(localConn *gonet.UDPConn) {
	defer localConn.Close()
	
	// 读取 Android 发来的 DNS 请求
	localConn.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, err := localConn.Read(buf)
	if err != nil {
		return
	}

	// 锁定 DNS 通道，确保同一时刻只有一个 DNS 请求在写入/读取，防止 TCP 流粘包
	s.dnsMu.Lock()
	defer s.dnsMu.Unlock()

	// 重试机制：如果 cached 连接已死，允许重连一次
	for i := 0; i < 2; i++ {
		// 1. 获取或建立连接
		if s.dnsConn == nil {
			// Dial
			proxyConn, err := s.dialer.Dial()
			if err != nil {
				log.Printf("[DNS] 代理连接失败: %v", err)
				return // 无法连接，直接丢弃
			}
			
			// Handshake (Target: 8.8.8.8:53)
			var payload []byte
			isVless := false
			
			switch strings.ToLower(s.config.Type) {
			case "mandala":
				client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
				payload, _ = client.BuildHandshakePayload("8.8.8.8", 53, s.config.Settings.Noise)
			case "trojan":
				payload, _ = protocol.BuildTrojanPayload(s.config.Password, "8.8.8.8", 53)
			case "vless":
				payload, _ = protocol.BuildVlessPayload(s.config.UUID, "8.8.8.8", 53)
				isVless = true
			case "shadowsocks":
				payload, _ = protocol.BuildShadowsocksPayload("8.8.8.8", 53)
			case "socks", "socks5":
				if err := protocol.HandshakeSocks5(proxyConn, s.config.Username, s.config.Password, "8.8.8.8", 53); err != nil {
					proxyConn.Close()
					log.Printf("[DNS] Socks5握手失败: %v", err)
					return
				}
			}

			if len(payload) > 0 {
				if _, err := proxyConn.Write(payload); err != nil {
					proxyConn.Close()
					continue // 重试
				}
			}

			if isVless {
				proxyConn = protocol.NewVlessConn(proxyConn)
			}
			
			s.dnsConn = proxyConn
		}

		// 2. 封装 DNS 请求 (Length-Prefixed)
		reqData := make([]byte, 2+n)
		reqData[0] = byte(n >> 8)
		reqData[1] = byte(n)
		copy(reqData[2:], buf[:n])

		s.dnsConn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := s.dnsConn.Write(reqData); err != nil {
			log.Printf("[DNS] 写入失败，重置连接: %v", err)
			s.dnsConn.Close()
			s.dnsConn = nil
			continue // 连接可能断开，重试 loop (i=1)
		}

		// 3. 读取响应长度
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(s.dnsConn, lenBuf); err != nil {
			log.Printf("[DNS] 读取长度失败: %v", err)
			s.dnsConn.Close()
			s.dnsConn = nil
			continue
		}
		respLen := int(lenBuf[0])<<8 | int(lenBuf[1])

		// 4. 读取响应体
		respBuf := make([]byte, respLen)
		if _, err := io.ReadFull(s.dnsConn, respBuf); err != nil {
			log.Printf("[DNS] 读取Body失败: %v", err)
			s.dnsConn.Close()
			s.dnsConn = nil
			continue
		}

		// 5. 写回 Android (UDP)
		localConn.Write(respBuf)
		
		// 成功，保持 dnsConn 不关闭，供下次使用
		return
	}
}

func (s *Stack) Close() {
	s.closeOnce.Do(func() {
		log.Println("[Stack] Stopping...")

		if s.cancel != nil {
			s.cancel()
		}
		
		// [优化] 关闭 DNS 缓存连接
		s.dnsMu.Lock()
		if s.dnsConn != nil {
			s.dnsConn.Close()
			s.dnsConn = nil
		}
		s.dnsMu.Unlock()

		time.Sleep(100 * time.Millisecond)

		if s.device != nil {
			s.device.Close()
		}

		if s.stack != nil {
			s.stack.Close()
		}

		log.Println("[Stack] Stopped.")
	})
}
