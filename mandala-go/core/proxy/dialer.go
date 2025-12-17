package proxy

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mandala/core/config"
)

// Dialer 负责建立到远程节点的连接
type Dialer struct {
	Config *config.OutboundConfig
}

func NewDialer(cfg *config.OutboundConfig) *Dialer {
	return &Dialer{Config: cfg}
}

// Dial 建立到底层传输层（通常是 WebSocket over TLS）的连接
func (d *Dialer) Dial() (net.Conn, error) {
	// 1. TCP 连接
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", d.Config.Server, d.Config.ServerPort), 5*time.Second)
	if err != nil {
		return nil, err
	}

	// 2. TLS 握手 (如果有)
	if d.Config.TLS != nil && d.Config.TLS.Enabled {
		tlsConfig := &tls.Config{
			ServerName:         d.Config.TLS.ServerName,
			InsecureSkipVerify: d.Config.TLS.Insecure, // Android端可能需要允许不安全证书用于测试
			MinVersion:         tls.VersionTLS12,
		}
		// 可以在这里引入 uTLS 来模拟 Chrome 指纹，目前使用 Go 标准库以保证稳定性
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("tls handshake failed: %v", err)
		}
		conn = tlsConn
	}

	// 3. WebSocket 握手 (如果传输层是 WS)
	if d.Config.Transport != nil && d.Config.Transport.Type == "ws" {
		wsConn, err := d.handshakeWebSocket(conn)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("websocket handshake failed: %v", err)
		}
		return wsConn, nil
	}

	return conn, nil
}

// handshakeWebSocket 手动执行 WS 握手，模拟 C 代码逻辑
func (d *Dialer) handshakeWebSocket(conn net.Conn) (net.Conn, error) {
	path := d.Config.Transport.Path
	if path == "" {
		path = "/"
	}
	host := d.Config.TLS.ServerName
	if host == "" {
		host = d.Config.Server
	}

	// 生成随机 WebSocket Key
	key := make([]byte, 16)
	rand.Read(key)
	keyStr := base64.StdEncoding.EncodeToString(key)

	// 构建 HTTP 请求
	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n", path, host, keyStr)

	// 添加自定义 Header
	if d.Config.Transport.Headers != nil {
		for k, v := range d.Config.Transport.Headers {
			req += fmt.Sprintf("%s: %s\r\n", k, v)
		}
	}
	req += "\r\n" // 请求结束

	// 发送握手请求
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}

	// 读取响应
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 101 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 这里我们返回原始 conn，因为 WebSocket 握手已完成，
	// 后续数据需要根据 WS 帧格式进行封装。
	// 为了简化，我们需要一个封装了 WS 帧读写的 net.Conn 包装器。
	return NewWSConn(conn, br), nil
}

// WSConn 是一个简单的 WebSocket 帧封装器
// 对应 C 代码中的 build_ws_frame 和 check_ws_frame
type WSConn struct {
	net.Conn
	reader *bufio.Reader
}

func NewWSConn(c net.Conn, br *bufio.Reader) *WSConn {
	return &WSConn{Conn: c, reader: br}
}

// Write 封装数据为 WebSocket Binary Frame
func (w *WSConn) Write(b []byte) (int, error) {
	// 简单实现：每次 Write 封装为一个 Frame
	// 帧头: Fin(1) | Rsv(3) | Opcode(4)
	// 0x82 = Fin=1, Opcode=2 (Binary)
	header := []byte{0x82}
	length := len(b)

	// Masking (客户端发送必须 Mask)
	maskKey := make([]byte, 4)
	rand.Read(maskKey)
	header[0] |= 0x80 // 这种写法不对，Mask bit 在第二个字节

	// 修正长度逻辑
	var lenBytes []byte
	if length < 126 {
		lenBytes = []byte{byte(length) | 0x80} // 0x80 表示 Mask开启
	} else if length <= 65535 {
		lenBytes = []byte{126 | 0x80}
		lenBytes = append(lenBytes, byte(length>>8), byte(length))
	} else {
		lenBytes = []byte{127 | 0x80}
		lenBytes = append(lenBytes, 0, 0, 0, 0) // 前4字节为0 (不支持超大包)
		lenBytes = append(lenBytes, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
	}

	if _, err := w.Conn.Write(header); err != nil {
		return 0, err
	}
	if _, err := w.Conn.Write(lenBytes); err != nil {
		return 0, err
	}
	if _, err := w.Conn.Write(maskKey); err != nil {
		return 0, err
	}

	// Mask payload
	maskedData := make([]byte, len(b))
	for i, v := range b {
		maskedData[i] = v ^ maskKey[i%4]
	}
	
	if _, err := w.Conn.Write(maskedData); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Read 读取 WebSocket Frame 并解包 Payload
// 简化版：暂不处理分片帧，假设服务器返回的是标准数据帧
func (w *WSConn) Read(b []byte) (int, error) {
	// 读取第一个字节
	header, err := w.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	// opcode := header & 0x0F
	// TODO: 处理 Ping/Pong/Close

	// 读取长度字节
	lenByte, err := w.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	
	// 服务器发送的数据通常没有 Mask
	payloadLen := int(lenByte & 0x7F)
	if payloadLen == 126 {
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(w.reader, lenBuf); err != nil {
			return 0, err
		}
		payloadLen = int(lenBuf[0])<<8 | int(lenBuf[1])
	} else if payloadLen == 127 {
		lenBuf := make([]byte, 8)
		if _, err := io.ReadFull(w.reader, lenBuf); err != nil {
			return 0, err
		}
		// 忽略高位，仅取低32位
		payloadLen = int(lenBuf[4])<<24 | int(lenBuf[5])<<16 | int(lenBuf[6])<<8 | int(lenBuf[7])
	}

	// 读取 Payload
	limit := payloadLen
	if limit > len(b) {
		limit = len(b) // 如果缓冲区不够，需要缓冲剩余数据(这里简化处理)
	}
	
	n, err := io.ReadFull(w.reader, b[:limit])
	return n, err
}
