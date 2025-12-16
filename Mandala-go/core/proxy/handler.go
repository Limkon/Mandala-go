package proxy

import (
	"fmt"
	"io"
	"net"

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

	// 1. SOCKS5 握手 (无需认证)
	// 读取版本号
	buf := make([]byte, 256)
	if _, err := io.ReadFull(localConn, buf[:2]); err != nil {
		return
	}
	// 只要是 SOCKS5 (0x05) 就回复无需认证 (0x00)
	localConn.Write([]byte{0x05, 0x00})

	// 2. 读取客户端请求 (CONNECT 目标地址)
	// 格式: Ver(1) Cmd(1) Rsv(1) Atyp(1) ...
	n, err := io.ReadFull(localConn, buf[:4])
	if err != nil || n < 4 {
		return
	}
	
	cmd := buf[1]
	atyp := buf[3]
	var targetHost string
	var targetPort int

	if cmd != 0x01 { // 仅支持 CONNECT
		return
	}

	// 解析目标地址
	switch atyp {
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		io.ReadFull(localConn, ipBuf)
		targetHost = net.IP(ipBuf).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(localConn, lenBuf)
		domainLen := int(lenBuf[0])
		domainBuf := make([]byte, domainLen)
		io.ReadFull(localConn, domainBuf)
		targetHost = string(domainBuf)
	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		io.ReadFull(localConn, ipBuf)
		targetHost = net.IP(ipBuf).String()
	}

	// 解析端口
	portBuf := make([]byte, 2)
	io.ReadFull(localConn, portBuf)
	targetPort = int(portBuf[0])<<8 | int(portBuf[1])

	// 3. 连接远程代理服务器
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		fmt.Printf("Dial remote failed: %v\n", err)
		return
	}
	defer remoteConn.Close()

	// 4. 发送协议头
	if h.Config.Type == "mandala" {
		client := protocol.NewMandalaClient(h.Config.Username, h.Config.Password)
		payload, err := client.BuildHandshakePayload(targetHost, targetPort)
		if err != nil {
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			return
		}
	} else {
		// TODO: 支持 VLESS/Trojan 等其他协议
		// 目前专注于 Mandala
	}

	// 5. 告知本地客户端连接成功
	// SOCKS5 响应: Ver(1) Rep(1) Rsv(1) Atyp(1) BindAddr... BindPort...
	// Rep=0x00 (Succeeded)
	localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 6. 双向转发
	go io.Copy(remoteConn, localConn)
	io.Copy(localConn, remoteConn)
}
