package tun

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

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
	log.Printf("=== Go Core Starting (FD: %d, MTU: %d) ===", fd, mtu)

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
	rawEndpoint := dev.LinkEndpoint()
	sniffedEndpoint := sniffer.New(rawEndpoint)

	if err := s.CreateNIC(nicID, sniffedEndpoint); err != nil {
		dev.Close()
		return nil, fmt.Errorf("create nic failed: %v", err)
	}

	// [關鍵修復] 同時添加 IPv4 和 IPv6 默認路由
	s.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
	})

	s.SetPromiscuousMode(nicID, true)

	ctx, cancel := context.WithCancel(context.Background())
	tStack := &Stack{
		stack:  s,
		device: dev,
		dialer: proxy.NewDialer(cfg),
		config: cfg,
		nat:    NewUDPNatManager(proxy.NewDialer(cfg), cfg),
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
		if r := recover(); r != nil {
			log.Printf("TCP Panic: %v", r)
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
		log.Printf("TCP Dial Failed: %v", dialErr)
		r.Complete(true)
		ep.Close()
		return
	}
	defer remoteConn.Close()

	// [協議握手邏輯]
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
		log.Printf("Handshake Error: %v", hErr)
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
	// ... UDP 處理邏輯保持不變 (見之前上傳的文件) ...
}

func (s *Stack) Close() {
	s.cancel()
	if s.device != nil { s.device.Close() }
	if s.stack != nil { s.stack.Close() }
}
