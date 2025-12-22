// 文件路径: mandala-go/core/proxy/handler.go

package proxy

import (
	"io"
	"log"
	"net"
	"strings"
	"time"

	"mandala/core/config"
	"mandala/core/protocol"
)

// Handler 处理单个本地连接
type Handler struct {
	Config *config.OutboundConfig
}

// HandleConnection 处理 SOCKS5 请求并转发
func (h *Handler) HandleConnection(localConn net.Conn) {
	defer localConn.Close()

	// 1. SOCKS5 握手阶段 (本地 App -> 本地核心)
	// 读取 [VER, NMETHODS]
	header := make([]byte, 2)
	if _, err := io.ReadFull(localConn, header); err != nil {
		return
	}
	if header[0] != 0x05 {
		return
	}

	// [关键修复] 必须消费掉客户端发送的所有 METHODS 列表
	// 如果不读取这部分，后续读取 Request 时会发生字节偏移，导致断流
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(localConn, methods); err != nil {
		return
	}

	// 告知本地客户端：无需认证 (0x00)
	// 注意：此处是本地握手，App 到核心之间通常不设密码
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. 读取客户端连接请求 (Request)
	// 格式: [VER, CMD, RSV, ATYP]
	requestHead := make([]byte, 4)
	if _, err := io.ReadFull(localConn, requestHead); err != nil {
		return
	}

	cmd := requestHead[1]
	atyp := requestHead[3]

	if cmd != 0x01 { // 仅支持 CONNECT 命令
		return
	}

	var targetHost string
	var targetPort int

	// 解析目标地址
	switch atyp {
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(localConn, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(localConn, lenBuf); err != nil {
			return
		}
		domainLen := int(lenBuf[0])
		domainBuf := make([]byte, domainLen)
		if _, err := io.ReadFull(localConn, domainBuf); err != nil {
			return
		}
		targetHost = string(domainBuf)
	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(localConn, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	default:
		return
	}

	// 读取端口 (2 bytes)
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(localConn, portBuf); err != nil {
		return
	}
	targetPort = int(portBuf[0])<<8 | int(portBuf[1])

	// 3. 连接远程代理服务器 (本地核心 -> 远程服务端)
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		log.Printf("[Proxy] 连接远程服务器失败: %v", err)
		// 告知本地客户端：连接失败 (0x04 Host unreachable)
		localConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	// 4. 执行远程协议握手
	proxyType := strings.ToLower(h.Config.Type)
	isVless := false

	switch proxyType {
	case "mandala":
		client := protocol.NewMandalaClient(h.Config.Username, h.Config.Password)
		payload, err := client.BuildHandshakePayload(targetHost, targetPort)
		if err == nil {
			remoteConn.Write(payload)
		}

	case "trojan":
		payload, err := protocol.BuildTrojanPayload(h.Config.Password, targetHost, targetPort)
		if err == nil {
			remoteConn.Write(payload)
		}

	case "vless":
		payload, err := protocol.BuildVlessPayload(h.Config.UUID, targetHost, targetPort)
		if err == nil {
			remoteConn.Write(payload)
		}
		isVless = true

	case "shadowsocks":
		payload, err := protocol.BuildShadowsocksPayload(targetHost, targetPort)
		if err == nil {
			remoteConn.Write(payload)
		}

	case "socks", "socks5":
		// 执行包含用户名密码认证的 SOCKS5 握手
		err := protocol.HandshakeSocks5(remoteConn, h.Config.Username, h.Config.Password, targetHost, targetPort)
		if err != nil {
			log.Printf("[Socks5] 远程握手失败: %v", err)
			return
		}
	}

	// 如果是 VLESS，包装连接以剥离响应头
	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	// 5. 告知本地客户端连接成功 (标准 10 字节响应)
	if _, err := localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	// 6. 双向转发流量
	localConn.SetDeadline(time.Time{})
	remoteConn.SetDeadline(time.Time{})

	errChan := make(chan error, 2)
	go func() {
		_, err := io.Copy(remoteConn, localConn)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(localConn, remoteConn)
		errChan <- err
	}()

	<-errChan
}
