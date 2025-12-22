package proxy

import (
	"io"
	"log"
	"net"
	"strings"
	"syscall"
	"time"

	"mandala/core/config"
	"mandala/core/protocol"
)

// Handler 处理单个本地连接
// (此文件如果被用于直连拨号或旧版本遗留，同样需要修复转发逻辑)
type Handler struct {
	Config *config.OutboundConfig
}

// HandleConnection 处理 SOCKS5 请求并转发
func (h *Handler) HandleConnection(localConn net.Conn) {
	defer localConn.Close()

	// 1. SOCKS5 握手 (无需认证)
	buf := make([]byte, 262)
	if _, err := io.ReadFull(localConn, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return
	}
	localConn.Write([]byte{0x05, 0x00})

	// 2. 读取客户端请求
	n, err := io.ReadFull(localConn, buf[:4])
	if err != nil || n < 4 {
		return
	}
	cmd := buf[1]
	atyp := buf[3]
	var targetHost string
	var targetPort int
	if cmd != 0x01 {
		return
	}

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
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(localConn, portBuf); err != nil {
		return
	}
	targetPort = int(portBuf[0])<<8 | int(portBuf[1])

	// 3. 连接远程代理服务器
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		log.Printf("[Proxy] Dial remote failed: %v", err)
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
		payload, err := client.BuildHandshakePayload(targetHost, targetPort)
		if err != nil {
			log.Printf("[Mandala] Build payload failed: %v", err)
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			log.Printf("[Mandala] Handshake write failed: %v", err)
			return
		}
	case "trojan":
		payload, err := protocol.BuildTrojanPayload(h.Config.Password, targetHost, targetPort)
		if err != nil {
			log.Printf("[Trojan] Build payload failed: %v", err)
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			log.Printf("[Trojan] Handshake write failed: %v", err)
			return
		}
	case "vless":
		payload, err := protocol.BuildVlessPayload(h.Config.UUID, targetHost, targetPort)
		if err != nil {
			log.Printf("[Vless] Build payload failed: %v", err)
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			log.Printf("[Vless] Handshake write failed: %v", err)
			return
		}
		isVless = true
	case "shadowsocks":
		payload, err := protocol.BuildShadowsocksPayload(targetHost, targetPort)
		if err != nil {
			log.Printf("[Shadowsocks] Build payload failed: %v", err)
			return
		}
		if _, err := remoteConn.Write(payload); err != nil {
			log.Printf("[Shadowsocks] Handshake write failed: %v", err)
			return
		}
	case "socks", "socks5":
		err := protocol.HandshakeSocks5(remoteConn, h.Config.Username, h.Config.Password, targetHost, targetPort)
		if err != nil {
			log.Printf("[Socks5] Handshake failed: %v", err)
			return
		}
	default:
		log.Println("[Proxy] Protocol not implemented:", proxyType)
		return
	}

	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	// 5. 告知本地客户端连接成功
	if _, err := localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	// 6. 双向转发 (优化版：处理半关闭)
	localConn.SetDeadline(time.Time{})
	remoteConn.SetDeadline(time.Time{})

	if tcpl, ok := localConn.(*net.TCPConn); ok {
		tcpl.SetNoDelay(true)
		tcpl.SetKeepAlive(true)
		tcpl.SetKeepAlivePeriod(30 * time.Second)
	}
	if tcpr, ok := remoteConn.(*net.TCPConn); ok {
		tcpr.SetNoDelay(true)
		tcpr.SetKeepAlive(true)
		tcpr.SetKeepAlivePeriod(30 * time.Second)
		if f, err := tcpr.File(); err == nil {
			_ = syscall.SetsockoptInt(int(f.Fd()), syscall.IPPROTO_TCP, syscall.TCP_MAXSEG, 1360)
		}
	}

	done := make(chan struct{}, 2)

	// 上行
	go func() {
		io.Copy(remoteConn, localConn)
		if tcpRemote, ok := remoteConn.(*net.TCPConn); ok {
			tcpRemote.CloseWrite()
		} else if cw, ok := remoteConn.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
		done <- struct{}{}
	}()

	// 下行
	go func() {
		io.Copy(localConn, remoteConn)
		if tcpLocal, ok := localConn.(*net.TCPConn); ok {
			tcpLocal.CloseWrite()
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	localConn.Close()
	remoteConn.Close()
}
