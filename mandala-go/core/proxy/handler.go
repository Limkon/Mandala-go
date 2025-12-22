// 文件路径: mandala-go/core/proxy/handler.go

package proxy

import (
	"bufio"
	"bytes"
	"fmt"
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

	// 设置初始读取超时，防止恶意连接挂起
	localConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// 使用 bufio.Reader 以便“偷看”第一个字节而不消耗它
	// 缓冲区大小设为 4KB，足够读取 HTTP 头部
	bufReader := bufio.NewReaderSize(localConn, 4096)

	// 偷看第一个字节
	firstByte, err := bufReader.Peek(1)
	if err != nil {
		return
	}

	// 恢复默认超时（后续由转发逻辑控制）
	localConn.SetReadDeadline(time.Time{})

	if firstByte[0] == 0x05 {
		// SOCKS5 协议
		h.handleSocks5(localConn, bufReader)
	} else {
		// 尝试作为 HTTP 协议处理
		h.handleHttp(localConn, bufReader)
	}
}

// handleSocks5 处理 SOCKS5 协议 (逻辑与 C 版本一致)
func (h *Handler) handleSocks5(localConn net.Conn, reader *bufio.Reader) {
	// 1. 握手阶段
	// 读取版本和方法数
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return
	}

	// 读取方法列表
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}

	// 回复：无需认证
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. 请求阶段
	requestHead := make([]byte, 4)
	if _, err := io.ReadFull(reader, requestHead); err != nil {
		return
	}

	if requestHead[1] != 0x01 { // 目前仅支持 CONNECT 命令
		return
	}

	var targetHost string
	var targetPort int
	atyp := requestHead[3]

	switch atyp {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		io.ReadFull(reader, ip)
		targetHost = net.IP(ip).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(reader, lenBuf)
		domain := make([]byte, int(lenBuf[0]))
		io.ReadFull(reader, domain)
		targetHost = string(domain)
	case 0x04: // IPv6 (Go 版本优势：C 版本此处主要丢弃，Go 可支持)
		ip := make([]byte, 16)
		io.ReadFull(reader, ip)
		targetHost = net.IP(ip).String()
	default:
		return
	}

	portBuf := make([]byte, 2)
	io.ReadFull(reader, portBuf)
	targetPort = int(portBuf[0])<<8 | int(portBuf[1])

	// 3. 建立隧道
	// 对于 SOCKS5，我们需要先回复客户端成功，再进行数据转发
	// 注意：这里我们先尝试连接远程，成功后再回复客户端，这比 C 版本稍微安全一点
	// C 版本是先回复成功再连接，Go 这里保持逻辑健壮性，若需完全一致可调整顺序
	
	// 连接远程节点
	remoteConn, err := h.dialRemote(targetHost, targetPort)
	if err != nil {
		// 告诉客户端连接失败
		localConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		log.Printf("[SOCKS5] 连接远程失败: %v", err)
		return
	}
	defer remoteConn.Close()

	// 告诉客户端连接成功
	localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 4. 双向转发
	h.forward(localConn, remoteConn, nil) // nil 表示没有预读的数据需要发送
}

// handleHttp 处理 HTTP/HTTPS 代理请求 (移植 C 版本逻辑)
func (h *Handler) handleHttp(localConn net.Conn, reader *bufio.Reader) {
	// 读取 HTTP 请求
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	targetHost := req.URL.Hostname()
	targetPort := 80
	portStr := req.URL.Port()
	if portStr != "" {
		p, _ := strconv.Atoi(portStr)
		if p > 0 {
			targetPort = p
		}
	} else {
		if req.Method == http.MethodConnect {
			targetPort = 443
		}
	}

	// 某些请求 Host 可能在 Header 里而不在 URL 里
	if targetHost == "" {
		if req.Host != "" {
			hostParts := strings.Split(req.Host, ":")
			targetHost = hostParts[0]
			if len(hostParts) > 1 {
				p, _ := strconv.Atoi(hostParts[1])
				if p > 0 {
					targetPort = p
				}
			}
		}
	}

	if targetHost == "" {
		return
	}

	// 连接远程节点
	remoteConn, err := h.dialRemote(targetHost, targetPort)
	if err != nil {
		log.Printf("[HTTP] 连接远程失败: %s:%d %v", targetHost, targetPort, err)
		return
	}
	defer remoteConn.Close()

	if req.Method == http.MethodConnect {
		// HTTPS 隧道：回复 200 OK，后续纯透传
		localConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		
		// 转发后续流量
		h.forward(localConn, remoteConn, nil)
	} else {
		// 普通 HTTP 请求：需要把刚才读出来的 Header 发给远程服务器
		// 因为 http.ReadRequest 消耗了缓冲区数据，我们需要重新组装
		// 但更简单的方法是将 Request 对象重新写入远程连接
		
		// 1. 将解析过的请求头写入 Buffer
		var buf bytes.Buffer
		req.Write(&buf) // 这会重构 HTTP 请求报文
		
		// 2. 将重构的报文发送给远程
		if _, err := remoteConn.Write(buf.Bytes()); err != nil {
			return
		}
		
		// 3. 转发后续流量（Body 等）
		h.forward(localConn, remoteConn, nil)
	}
}

// dialRemote 统一的远程连接和握手逻辑
func (h *Handler) dialRemote(host string, port int) (net.Conn, error) {
	dialer := NewDialer(h.Config)
	remoteConn, err := dialer.Dial()
	if err != nil {
		return nil, err
	}

	// 设置握手超时
	remoteConn.SetDeadline(time.Now().Add(15 * time.Second))
	
	proxyType := strings.ToLower(h.Config.Type)
	var finalConn net.Conn = remoteConn
	var handshakeErr error

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
		_, handshakeErr = remoteConn.Write(payload)
		finalConn = protocol.NewVlessConn(remoteConn) // VLESS 需要特殊的流处理

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

	// 清除超时
	remoteConn.SetDeadline(time.Time{})
	return finalConn, nil
}

// forward 双向数据转发
func (h *Handler) forward(local net.Conn, remote net.Conn, initialData []byte) {
	// 如果有预先读取的数据，先发送给远程
	if len(initialData) > 0 {
		if _, err := remote.Write(initialData); err != nil {
			return
		}
	}

	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(remote, local)
		errChan <- err
		// 确保关闭，触发另一端退出
		remote.Close()
		local.Close()
	}()

	go func() {
		_, err := io.Copy(local, remote)
		errChan <- err
		// 确保关闭，触发另一端退出
		local.Close()
		remote.Close()
	}()

	<-errChan
}
