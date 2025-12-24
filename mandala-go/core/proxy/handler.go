package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"mandala/core/config"
	"mandala/core/protocol"
)

// Handler 处理单个本地连接的请求
type Handler struct {
	Config *config.OutboundConfig
}

// HandleConnection 处理 SOCKS5 请求并转发流量
func (h *Handler) HandleConnection(localConn net.Conn) {
	defer localConn.Close()

	// 设置本地读取超时，防止恶意连接占用资源
	localConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// 1. SOCKS5 握手认证阶段
	buf := make([]byte, 256)
	n, err := io.ReadAtLeast(localConn, buf, 2)
	if err != nil {
		return
	}

	// SOCKS 版本检查 (0x05)
	if buf[0] != 0x05 {
		return
	}

	// 响应无需认证 (0x05 0x00)
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. 获取请求详情 (CMD)
	n, err = io.ReadAtLeast(localConn, buf, 4)
	if err != nil {
		return
	}

	// 仅支持 CONNECT 命令 (0x01)
	if buf[1] != 0x01 {
		return
	}

	var targetHost string
	var targetPort int

	// 解析目标地址类型
	switch buf[3] {
	case 0x01: // IPv4
		if n < 10 {
			if _, err := io.ReadFull(localConn, buf[n:10]); err != nil {
				return
			}
		}
		targetHost = net.IP(buf[4:8]).String()
		targetPort = int(binary.BigEndian.Uint16(buf[8:10]))
	case 0x03: // 域名
		domainLen := int(buf[4])
		required := 5 + domainLen + 2
		if n < required {
			if _, err := io.ReadFull(localConn, buf[n:required]); err != nil {
				return
			}
		}
		targetHost = string(buf[5 : 5+domainLen])
		targetPort = int(binary.BigEndian.Uint16(buf[5+domainLen : 5+domainLen+2]))
	case 0x04: // IPv6
		if n < 22 {
			if _, err := io.ReadFull(localConn, buf[n:22]); err != nil {
				return
			}
		}
		targetHost = net.IP(buf[4:20]).String()
		targetPort = int(binary.BigEndian.Uint16(buf[20:22]))
	default:
		return
	}

	// 重置超时，准备数据传输
	localConn.SetDeadline(time.Time{})

	// 3. 连接远程代理服务器 (Dialer 内部会处理 Fragment 分片)
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		log.Printf("[Proxy] 连接远程服务器失败: %v", err)
		// 告知客户端连接失败
		localConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	// 4. 发送协议头 (握手)
	proxyType := strings.ToLower(h.Config.Type)
	isVless := false

	switch proxyType {
	case "mandala":
		client := protocol.NewMandalaClient(h.Config.Username, h.Config.Password)

		// [修改] 获取随机填充大小配置
		noiseSize := 0
		if h.Config.Settings != nil && h.Config.Settings.Noise {
			noiseSize = h.Config.Settings.NoiseSize
		}

		// [修改] 传入 noiseSize 进行握手包构建
		payload, err := client.BuildHandshakePayload(targetHost, targetPort, noiseSize)
		if err != nil {
			log.Printf("[Mandala] 构造握手包失败: %v", err)
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			log.Printf("[Mandala] 发送握手包失败: %v", err)
			return
		}

	case "trojan":
		payload, err := protocol.BuildTrojanPayload(h.Config.Password, targetHost, targetPort)
		if err != nil {
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			return
		}

	case "vless":
		payload, err := protocol.BuildVlessPayload(h.Config.UUID, targetHost, targetPort)
		if err != nil {
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			return
		}
		isVless = true

	case "shadowsocks":
		payload, err := protocol.BuildShadowsocksPayload(targetHost, targetPort)
		if err != nil {
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			return
		}

	case "socks", "socks5":
		err := protocol.HandshakeSocks5(remoteConn, h.Config.Username, h.Config.Password, targetHost, targetPort)
		if err != nil {
			return
		}

	default:
		log.Println("[Proxy] 未实现的协议类型:", proxyType)
		return
	}

	// 如果是 VLESS 协议，需要包装连接以处理响应头
	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	// 5. 告知本地客户端连接成功 (响应 SOCKS5 Success)
	if _, err := localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	// 6. 双向数据转发
	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(remoteConn, localConn)
		errChan <- err
	}()

	go func() {
		_, err := io.Copy(localConn, remoteConn)
		errChan <- err
	}()

	// 等待任意一方断开连接
	<-errChan
}
