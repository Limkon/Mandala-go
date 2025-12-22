// 文件路径: mandala-go/core/proxy/handler.go

package proxy

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mandala/core/config"
	"mandala/core/protocol"
)

// Handler 处理单个本地连接
type Handler struct {
	Config *config.OutboundConfig
}

// HandleConnection 入口函数：识别协议并分流
func (h *Handler) HandleConnection(localConn net.Conn) {
	defer localConn.Close()

	// 1. 设置初始读取超时，防止恶意连接挂起
	localConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// 使用 bufio.Reader 进行协议探测
	// 关键修复：这个 reader 实例必须贯穿整个 local -> remote 的读取过程
	// 否则 Peek 或 ReadRequest 读走的数据会在转发时丢失
	bufReader := bufio.NewReaderSize(localConn, 4096)

	// 偷看第一个字节以判断协议
	firstByte, err := bufReader.Peek(1)
	if err != nil {
		return
	}

	// 恢复默认超时（后续由转发逻辑控制）
	localConn.SetReadDeadline(time.Time{})

	if firstByte[0] == 0x05 {
		// SOCKS5 协议处理
		h.handleSocks5(localConn, bufReader)
	} else {
		// HTTP 协议处理（包括 CONNECT 和普通 GET/POST）
		h.handleHttp(localConn, bufReader)
	}
}

// handleSocks5 处理 SOCKS5 协议 (逻辑与 C 版本对齐，修复了数据流丢失问题)
func (h *Handler) handleSocks5(localConn net.Conn, reader *bufio.Reader) {
	// 1. 握手阶段：协商认证方法
	// 读取版本号和方法数量
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return
	}

	// 读取方法列表并忽略（目前仅支持无需认证）
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}

	// 回复客户端：选择“无需认证”方法 (0x05 0x00)
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. 请求阶段：读取 SOCKS5 CONNECT 请求
	// 读取前 4 字节: [VER, CMD, RSV, ATYP]
	requestHead := make([]byte, 4)
	if _, err := io.ReadFull(reader, requestHead); err != nil {
		return
	}

	if requestHead[1] != 0x01 { // 仅支持 CONNECT 命令
		return
	}

	var targetHost string
	var targetPort int
	atyp := requestHead[3]

	// 解析目标地址
	switch atyp {
	case 0x01: // IPv4 地址
		ip := make([]byte, 4)
		io.ReadFull(reader, ip)
		targetHost = net.IP(ip).String()
	case 0x03: // 域名
		lenBuf := make([]byte, 1)
		io.ReadFull(reader, lenBuf)
		domainLen := int(lenBuf[0])
		domain := make([]byte, domainLen)
		io.ReadFull(reader, domain)
		targetHost = string(domain)
	case 0x04: // IPv6 地址
		ip := make([]byte, 16)
		io.ReadFull(reader, ip)
		targetHost = net.IP(ip).String()
	default:
		return
	}

	// 读取 2 字节端口号
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBuf); err != nil {
		return
	}
	targetPort = int(portBuf[0])<<8 | int(portBuf[1])

	// 3. 建立远程连接
	// 使用统一的 dialRemote 逻辑连接到服务器节点并完成协议握手
	remoteConn, err := h.dialRemote(targetHost, targetPort)
	if err != nil {
		// 通知本地客户端连接失败 (0x05 0x04: 主机不可达)
		localConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		log.Printf("[SOCKS5] 连接远程失败: %v", err)
		return
	}
	defer remoteConn.Close()

	// 通知本地客户端连接成功 (0x05 0x00: 成功)
	localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 4. 双向转发
	// 关键修复：从 reader 转发数据到 remote，确保不会丢失缓冲区内已读取的数据
	h.forward(localConn, reader, remoteConn)
}
// handleHttp 处理 HTTP 代理请求 (包括 HTTPS CONNECT 和普通 HTTP 请求)
func (h *Handler) handleHttp(localConn net.Conn, reader *bufio.Reader) {
	// 1. 使用 http.ReadRequest 从具有缓冲的 reader 中解析请求
	// 这确保了即使部分数据已被读取到缓冲区，解析依然正确
	req, err := http.ReadRequest(reader)
	if err != nil {
		log.Printf("[HTTP] 解析请求失败: %v", err)
		return
	}

	// 2. 提取目标主机和端口
	targetHost := req.URL.Hostname()
	targetPort := 80
	if portStr := req.URL.Port(); portStr != "" {
		p, _ := strconv.Atoi(portStr)
		if p > 0 {
			targetPort = p
		}
	} else if req.Method == http.MethodConnect {
		targetPort = 443
	}

	// 如果 URL 中未指定 Host，则从 Header 中提取
	if targetHost == "" && req.Host != "" {
		hostParts := strings.Split(req.Host, ":")
		targetHost = hostParts[0]
		if len(hostParts) > 1 {
			p, _ := strconv.Atoi(hostParts[1])
			if p > 0 {
				targetPort = p
			}
		}
	}

	if targetHost == "" {
		return
	}

	// 3. 连接远程节点
	remoteConn, err := h.dialRemote(targetHost, targetPort)
	if err != nil {
		log.Printf("[HTTP] 连接远程失败: %s:%d %v", targetHost, targetPort, err)
		return
	}
	defer remoteConn.Close()

	// 4. 根据请求方法处理后续逻辑
	if req.Method == http.MethodConnect {
		// HTTPS 隧道：回复客户端 200 OK
		if _, err := localConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}
		// 开始双向转发
		h.forward(localConn, reader, remoteConn)
	} else {
		// 普通 HTTP 请求：需要将已解析并从缓冲区读出的请求重新发送给远程节点
		var buf bytes.Buffer
		if err := req.Write(&buf); err != nil {
			return
		}
		if _, err := remoteConn.Write(buf.Bytes()); err != nil {
			return
		}
		// 继续转发后续的 Body 或其他流数据
		h.forward(localConn, reader, remoteConn)
	}
}

// dialRemote 统一处理远程代理服务器的连接与握手流程
func (h *Handler) dialRemote(host string, port int) (net.Conn, error) {
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		return nil, err
	}

	// 设置握手超时时间，防止连接卡死
	remoteConn.SetDeadline(time.Now().Add(15 * time.Second))
	
	proxyType := strings.ToLower(h.Config.Type)
	var finalConn net.Conn = remoteConn
	var handshakeErr error

	// 执行对应的远程协议握手
	switch proxyType {
	case "mandala":
		client := protocol.NewMandalaClient(h.Config.Username, h.Config.Password)
		payload, _ := client.BuildHandshakePayload(host, port)
		_, handshakeErr = remoteConn.Write(payload)

	case "trojan":
		payload, _ := protocol.BuildTrojanPayload(h.Config.Password, host, port)
		_, handshakeErr = remoteConn.Write(payload)

	case "vless":
		payload, _ := protocol.BuildVlessPayload(h.Config.UUID, host, port)
		if _, handshakeErr = remoteConn.Write(payload); handshakeErr == nil {
			// VLESS 响应头剥离由 VlessConn 处理
			finalConn = protocol.NewVlessConn(remoteConn)
		}

	case "shadowsocks":
		payload, _ := protocol.BuildShadowsocksPayload(host, port)
		_, handshakeErr = remoteConn.Write(payload)

	case "socks", "socks5":
		handshakeErr = protocol.HandshakeSocks5(remoteConn, h.Config.Username, h.Config.Password, host, port)
	}

	if handshakeErr != nil {
		remoteConn.Close()
		return nil, handshakeErr
	}

	// 握手成功，清除超时设置
	remoteConn.SetDeadline(time.Time{})
	return finalConn, nil
}

// forward 实现高效的双向数据转发
// 注意：localReader 必须是 HandleConnection 中创建的 bufio.Reader，以防数据丢失
func (h *Handler) forward(local net.Conn, localReader *bufio.Reader, remote net.Conn) {
	errChan := make(chan error, 2)

	// 协程1: 本地 -> 远程 (从具有缓冲区的 localReader 读取)
	go func() {
		_, err := io.Copy(remote, localReader)
		errChan <- err
		remote.Close()
		local.Close()
	}()

	// 协程2: 远程 -> 本地
	go func() {
		_, err := io.Copy(local, remote)
		errChan <- err
		local.Close()
		remote.Close()
	}()

	// 等待任一方向结束
	<-errChan
}
