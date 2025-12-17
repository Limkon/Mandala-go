package proxy

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
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
	targetAddr := fmt.Sprintf("%s:%d", d.Config.Server, d.Config.ServerPort)
	conn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
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
		// 如果未设置 SNI，默认使用服务器地址
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = d.Config.Server
		}
		
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

// handshakeWebSocket 执行标准 WebSocket 握手
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
	// 注意: Golang 的 http.WriteRequest 可以自动处理，但为了轻量级直接构造字符串
	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"User-Agent: Go-Mandala-Client/1.0\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n", path, host, keyStr)

	// 添加自定义 Header (如 Host, User-Agent 等覆盖)
	if d.Config.Transport.Headers != nil {
		for k, v := range d.Config.Transport.Headers {
			req += fmt.Sprintf("%s: %s\r\n", k, v)
		}
	}
	req += "\r\n"

	// 发送握手请求
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}

	// 读取响应
	// 使用 bufio 是因为后续 WebSocket 帧读取也需要缓冲
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 101 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	// 简单的客户端通常忽略 Sec-WebSocket-Accept 校验

	return NewWSConn(conn, br), nil
}

// WSConn 封装 WebSocket 帧读写，实现 net.Conn 接口
type WSConn struct {
	net.Conn
	reader    *bufio.Reader
	remaining int64 // 当前帧剩余未读取的 Payload 字节数
}

func NewWSConn(c net.Conn, br *bufio.Reader) *WSConn {
	return &WSConn{Conn: c, reader: br, remaining: 0}
}

// Write 将数据封装为 masked binary frame 发送
func (w *WSConn) Write(b []byte) (int, error) {
	length := len(b)
	if length == 0 {
		return 0, nil
	}

	// Frame Head: FIN=1, Opcode=2 (Binary) -> 0x82
	// Server 可能会断开连接如果客户端发送未掩码的数据，所以必须 Mask
	
	// 1. 准备头部缓冲区 (最大 header 14 bytes)
	buf := make([]byte, 0, 14+length)
	
	// Byte 0: FIN | RSV | OPCODE
	buf = append(buf, 0x82)

	// Byte 1: MASK(1) | PAYLOAD LEN(7)
	// 客户端发送必须置 MASK 位 (0x80)
	if length < 126 {
		buf = append(buf, byte(length)|0x80)
	} else if length <= 65535 {
		buf = append(buf, 126|0x80)
		buf = binary.BigEndian.AppendUint16(buf, uint16(length))
	} else {
		buf = append(buf, 127|0x80)
		buf = binary.BigEndian.AppendUint64(buf, uint64(length))
	}

	// Mask Key (4 bytes)
	maskKey := make([]byte, 4)
	rand.Read(maskKey)
	buf = append(buf, maskKey...)

	// Append Masked Payload
	// 为了性能，不再次分配内存，直接在 buf 后追加
	payloadStart := len(buf)
	buf = append(buf, b...)
	
	// 执行 XOR Mask
	// 注意: buf[payloadStart:] 就是刚才 append 的 b 的副本
	for i := 0; i < length; i++ {
		buf[payloadStart+i] ^= maskKey[i%4]
	}

	// 发送整个帧
	if _, err := w.Conn.Write(buf); err != nil {
		return 0, err
	}

	return length, nil
}

// Read 读取 WebSocket Payload
// 自动处理分帧、跳过控制帧(Ping/Pong)
func (w *WSConn) Read(b []byte) (int, error) {
	for {
		// 1. 如果当前帧还有剩余数据，直接读取
		if w.remaining > 0 {
			limit := int64(len(b))
			if w.remaining < limit {
				limit = w.remaining
			}
			n, err := w.reader.Read(b[:limit])
			if n > 0 {
				w.remaining -= int64(n)
			}
			return n, err
		}

		// 2. 读取新帧头部
		// 读取第一个字节 (FIN, RSV, Opcode)
		header, err := w.reader.ReadByte()
		if err != nil {
			return 0, err
		}
		
		// fin := header & 0x80
		opcode := header & 0x0F

		// 读取第二个字节 (Mask, Length)
		lenByte, err := w.reader.ReadByte()
		if err != nil {
			return 0, err
		}

		masked := (lenByte & 0x80) != 0
		payloadLen := int64(lenByte & 0x7F)

		if payloadLen == 126 {
			lenBuf := make([]byte, 2)
			if _, err := io.ReadFull(w.reader, lenBuf); err != nil {
				return 0, err
			}
			payloadLen = int64(binary.BigEndian.Uint16(lenBuf))
		} else if payloadLen == 127 {
			lenBuf := make([]byte, 8)
			if _, err := io.ReadFull(w.reader, lenBuf); err != nil {
				return 0, err
			}
			payloadLen = int64(binary.BigEndian.Uint64(lenBuf))
		}

		// 读取 Mask Key (如果 Server 发送了 Mask，虽然标准不建议，但兼容处理)
		var maskKey []byte
		if masked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(w.reader, maskKey); err != nil {
				return 0, err
			}
		}

		// 处理控制帧
		if opcode == 0x8 { // Close
			return 0, io.EOF
		}
		if opcode == 0x9 { // Ping
			// 读取 Payload 并丢弃 (或者回复 Pong，这里简化为丢弃)
			if payloadLen > 0 {
				if _, err := io.CopyN(io.Discard, w.reader, payloadLen); err != nil {
					return 0, err
				}
			}
			// 可选: 回复 Pong
			// w.WriteControl(Pong...)
			continue // 继续读取下一帧
		}
		if opcode == 0xA { // Pong
			if payloadLen > 0 {
				io.CopyN(io.Discard, w.reader, payloadLen)
			}
			continue
		}

		// 数据帧 (Text 0x1 或 Binary 0x2)
		// 注意: 这里未处理 Mask 解码 (Server -> Client 通常不 Mask)
		// 如果 Server 确实 Mask 了，需要在这里解密，但极少见
		
		w.remaining = payloadLen
		
		// 循环回到开始，执行读取
	}
}
