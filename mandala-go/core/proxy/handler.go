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

	// 1. 本地 SOCKS5 握手 (App -> 本地核心)
	// 设置 5 秒超时，确保本地通信不卡死
	localConn.SetDeadline(time.Now().Add(5 * time.Second))
	
	header := make([]byte, 2)
	if _, err := io.ReadFull(localConn, header); err != nil {
		return
	}
	if header[0] != 0x05 {
		return
	}

	// [关键修复] 必须消费掉所有方法列表，防止位元组残留在缓冲区
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(localConn, methods); err != nil {
		return
	}

	// 回复无需认证
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. 读取客户端连接请求
	requestHead := make([]byte, 4)
	if _, err := io.ReadFull(localConn, requestHead); err != nil {
		return
	}

	if requestHead[1] != 0x01 { // 仅支持 CONNECT
		return
	}

	var targetHost string
	var targetPort int
	atyp := requestHead[3]

	// 解析地址
	switch atyp {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		io.ReadFull(localConn, ip)
		targetHost = net.IP(ip).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(localConn, lenBuf)
		domain := make([]byte, int(lenBuf[0]))
		io.ReadFull(localConn, domain)
		targetHost = string(domain)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		io.ReadFull(localConn, ip)
		targetHost = net.IP(ip).String()
	default:
		return
	}

	portBuf := make([]byte, 2)
	io.ReadFull(localConn, portBuf)
	targetPort = int(portBuf[0])<<8 | int(portBuf[1])

	// 3. 连接远程代理服务器 (WebSocket)
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		log.Printf("[Proxy] 连接远程失败: %v", err)
		localConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	// 4. 执行远程协议握手
	// [优化] 针对 Cloudflare Worker 的延迟，远程握手超时设为 15 秒
	remoteConn.SetDeadline(time.Now().Add(15 * time.Second))
	
	proxyType := strings.ToLower(h.Config.Type)
	isVless := false

	switch proxyType {
	case "mandala":
		client := protocol.NewMandalaClient(h.Config.Username, h.Config.Password)
		payload, _ := client.BuildHandshakePayload(targetHost, targetPort)
		remoteConn.Write(payload)

	case "trojan":
		payload, _ := protocol.BuildTrojanPayload(h.Config.Password, targetHost, targetPort)
		remoteConn.Write(payload)

	case "vless":
		payload, _ := protocol.BuildVlessPayload(h.Config.UUID, targetHost, targetPort)
		remoteConn.Write(payload)
		isVless = true

	case "shadowsocks":
		payload, _ := protocol.BuildShadowsocksPayload(targetHost, targetPort)
		remoteConn.Write(payload)

	case "socks", "socks5":
		err := protocol.HandshakeSocks5(remoteConn, h.Config.Username, h.Config.Password, targetHost, targetPort)
		if err != nil {
			log.Printf("[Socks5] 远程握手失败: %v", err)
			return
		}
	}

	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	// 5. 握手成功，告知本地 App
	localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 6. 清除超时，进入双向转发
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
