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

func init() {
	// [修复] 初始化随机数种子
	rand.Seed(time.Now().UnixNano())
}

type Dialer struct {
	Config *config.OutboundConfig
}

func NewDialer(cfg *config.OutboundConfig) *Dialer {
	return &Dialer{Config: cfg}
}

func (d *Dialer) Dial() (net.Conn, error) {
	targetAddr := fmt.Sprintf("%s:%d", d.Config.Server, d.Config.ServerPort)
	conn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}

	// [修复] 启用 TCP KeepAlive，防止连接在无数据时被运营商或防火墙切断
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}

	if d.Config.TLS != nil && d.Config.TLS.Enabled {
		tlsConfig := &tls.Config{
			ServerName:         d.Config.TLS.ServerName,
			InsecureSkipVerify: d.Config.TLS.Insecure,
			MinVersion:         tls.VersionTLS12,
		}
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

func (d *Dialer) handshakeWebSocket(conn net.Conn) (net.Conn, error) {
	path := d.Config.Transport.Path
	if path == "" { path = "/" }
	
	// [修复] 安全获取 Host，防止 TLS 配置为空时崩溃
	host := d.Config.Server
	if d.Config.TLS != nil && d.Config.TLS.ServerName != "" {
		host = d.Config.TLS.ServerName
	}

	key := make([]byte, 16)
	rand.Read(key)
	keyStr := base64.StdEncoding.EncodeToString(key)

	// [修复] 添加 User-Agent 头
	// 许多服务器和 CDN (如 Cloudflare) 会拒绝没有 UA 的 WebSocket 请求。
	// 这里硬编码一个常见的 Chrome UA，与 C 版本行为保持一致。
	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n", path, host, keyStr)

	if d.Config.Transport.Headers != nil {
		for k, v := range d.Config.Transport.Headers {
			req += fmt.Sprintf("%s: %s\r\n", k, v)
		}
	}
	req += "\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}

	br := bufio.NewReader(conn)
	// 使用 http.ReadResponse 读取响应，它会自动处理 Header 结束符
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 101 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return NewWSConn(conn, br), nil
}

type WSConn struct {
	net.Conn
	reader    *bufio.Reader
	remaining int64 
}

func NewWSConn(c net.Conn, br *bufio.Reader) *WSConn {
	return &WSConn{Conn: c, reader: br, remaining: 0}
}

func (w *WSConn) Write(b []byte) (int, error) {
	length := len(b)
	if length == 0 { return 0, nil }

	// 预分配 buffer，避免多次 append 导致内存拷贝
	buf := make([]byte, 0, 14+length)
	buf = append(buf, 0x82) // Binary Frame

	if length < 126 {
		buf = append(buf, byte(length)|0x80)
	} else if length <= 65535 {
		buf = append(buf, 126|0x80)
		buf = binary.BigEndian.AppendUint16(buf, uint16(length))
	} else {
		buf = append(buf, 127|0x80)
		buf = binary.BigEndian.AppendUint64(buf, uint64(length))
	}

	maskKey := make([]byte, 4)
	rand.Read(maskKey)
	buf = append(buf, maskKey...)

	payloadStart := len(buf)
	buf = append(buf, b...)
	
	// XOR Masking
	for i := 0; i < length; i++ {
		buf[payloadStart+i] ^= maskKey[i%4]
	}

	if _, err := w.Conn.Write(buf); err != nil {
		return 0, err
	}
	return length, nil
}

func (w *WSConn) Read(b []byte) (int, error) {
	for {
		// 1. 如果当前帧还有剩余数据未读，直接读取 payload
		if w.remaining > 0 {
			limit := int64(len(b))
			if w.remaining < limit { limit = w.remaining }
			n, err := w.reader.Read(b[:limit])
			if n > 0 { w.remaining -= int64(n) }
			if n > 0 || err != nil { return n, err }
		}

		// 2. 读取新帧头部
		header, err := w.reader.ReadByte()
		if err != nil { return 0, err }
		
		opcode := header & 0x0F
		lenByte, err := w.reader.ReadByte()
		if err != nil { return 0, err }

		masked := (lenByte & 0x80) != 0
		payloadLen := int64(lenByte & 0x7F)

		if payloadLen == 126 {
			lenBuf := make([]byte, 2)
			if _, err := io.ReadFull(w.reader, lenBuf); err != nil { return 0, err }
			payloadLen = int64(binary.BigEndian.Uint16(lenBuf))
		} else if payloadLen == 127 {
			lenBuf := make([]byte, 8)
			if _, err := io.ReadFull(w.reader, lenBuf); err != nil { return 0, err }
			payloadLen = int64(binary.BigEndian.Uint64(lenBuf))
		}

		// 忽略 Mask (服务端发回的数据通常不 Mask，但也可能有，忽略 Mask Key 即可)
		if masked {
			if _, err := io.Discard.Write(make([]byte, 4)); err != nil { 
				// io.Discard Write always returns nil error, but reading from reader might fail
				// Better way to skip 4 bytes:
				maskBuf := make([]byte, 4)
				if _, err := io.ReadFull(w.reader, maskBuf); err != nil { return 0, err }
			}
		}

		// 处理控制帧
		switch opcode {
		case 0x8: // Close Frame
			return 0, io.EOF
		case 0x9, 0xA: // Ping / Pong
			if payloadLen > 0 {
				io.CopyN(io.Discard, w.reader, payloadLen)
			}
			continue
		case 0x0, 0x1, 0x2: // Continuation, Text, Binary
			w.remaining = payloadLen
			if w.remaining == 0 { continue }
			// 循环回到步骤 1 读取数据
		default:
			// 未知帧，丢弃
			if payloadLen > 0 {
				io.CopyN(io.Discard, w.reader, payloadLen)
			}
			continue
		}
	}
}
