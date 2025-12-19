package tun

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"      // 用於 handleUDP 邏輯
	"strings"
	"time"     // 用於超時處理

	"mandala/core/config"
	"mandala/core/protocol"
	"mandala/core/proxy"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/sniffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

func init() {
	log.SetPrefix("Go日志: ")
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
	log.Printf("=== Go核心启动 (文件描述符: %d, MTU: %d) ===", fd, mtu)

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

	nicID := tcpip.NICID(1)
	if err := s.CreateNIC(nicID, sniffer.New(dev.LinkEndpoint())); err != nil {
		dev.Close()
		return nil, fmt.Errorf("创建网卡失败: %v", err)
	}

	s.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
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
			log.Printf("TCP恐慌恢复: %v", err)
		}
	}()

	id := r.ID()
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		r.Complete(true)
		return
	}

	remoteConn, dialErr := s.dialer.Dial()
	if dialErr != nil {
		log.Printf("TCP拨号失败 (%s:%d): %v", id.LocalAddress, id.LocalPort, dialErr)
		r.Complete(true)
		ep.Close()
		return
	}
	defer remoteConn.Close()

	var payload []byte
	var hErr error
	targetHost := id.LocalAddress.String()
	targetPort := int(id.LocalPort)

	switch strings.ToLower(s.config.Type) {
	case "mandala":
		client := protocol.NewMandalaClient(s.config.Username, s.config.Password)
		payload, hErr = client.BuildHandshakePayload(targetHost, targetPort)
	case "trojan":
		payload, hErr = protocol.BuildTrojanPayload(s.config.Password, targetHost, targetPort)
	case "vless":
		payload, hErr = protocol.BuildVlessPayload(s.config.UUID, targetHost, targetPort)
	}

	if hErr != nil {
		log.Printf("协议握手包构造失败: %v", hErr)
		r.Complete(true)
		return
	}

	if len(payload) > 0 {
		remoteConn.Write(payload)
	}

	r.Complete(false)
	localConn := gonet.NewTCPConn(&wq, ep)
	defer localConn.Close()

	go io.Copy(remoteConn, localConn)
	io.Copy(localConn, remoteConn)
}

func (s *Stack) handleUDP(r *udp.ForwarderRequest) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("UDP恐慌恢复: %v", err)
		}
	}()

	id := r.ID()
	targetPort := int(id.LocalPort)
    
	if targetPort == 53 {
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil { return }
		localConn := gonet.NewUDPConn(s.stack, &wq, ep)
		go s.handleLocalDNS(localConn)
		return
	}

	targetIP := net.IP(id.LocalAddress.AsSlice()).String() // 确保 net 包被使用
	srcKey := fmt.Sprintf("%s:%d->%s:%d", id.RemoteAddress.String(), id.RemotePort, targetIP, targetPort)

	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil { return }

	localConn := gonet.NewUDPConn(s.stack, &wq, ep)
	session, natErr := s.nat.GetOrCreate(srcKey, localConn, targetIP, targetPort)
	if natErr != nil {
		log.Printf("UDP NAT会话创建失败: %v", natErr)
		localConn.Close()
		return
	}

	go func() {
		defer localConn.Close()
		buf := make([]byte, 4096)
		for {
			// 确保 time 包被使用
			localConn.SetDeadline(time.Now().Add(60 * time.Second))
			n, rErr := localConn.Read(buf)
			if rErr != nil { return }
			if _, wErr := session.RemoteConn.Write(buf[:n]); wErr != nil { return }
		}
	}()
}

func (s *Stack) handleLocalDNS(conn *gonet.UDPConn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil { return }

	realDNS := "223.5.5.5:53"
	tcpConn, err := net.DialTimeout("tcp", realDNS, 3*time.Second)
	if err != nil { return }
	defer tcpConn.Close()

	reqData := make([]byte, 2+n)
	reqData[0] = byte(n >> 8)
	reqData[1] = byte(n)
	copy(reqData[2:], buf[:n])

	if _, err := tcpConn.Write(reqData); err != nil { return }

	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(tcpConn, lenBuf); err != nil { return }
	respLen := int(lenBuf[0])<<8 | int(lenBuf[1])

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(tcpConn, respBuf); err != nil { return }
	conn.Write(respBuf)
}

func (s *Stack) Close() {
	s.cancel()
	if s.device != nil { s.device.Close() }
	if s.stack != nil { s.stack.Close() }
	log.Println("Go核心栈已关闭")
}
