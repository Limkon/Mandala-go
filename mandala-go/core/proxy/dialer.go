package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"mandala/core/config"

	"github.com/miekg/dns"
	utls "github.com/refraction-networking/utls"
)

func init() {
	// 初始化随机数种子
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

	if d.Config.TLS != nil && d.Config.TLS.Enabled {
		// [Step 1] 准备 ECH 配置
		var echConfigList []byte
		if d.Config.TLS.EnableECH && d.Config.TLS.ECHDoHURL != "" && d.Config.TLS.ECHPublicName != "" {
			// 使用带超时的 Context 防止 DNS 查询卡死
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			configs, err := resolveECHConfig(ctx, d.Config.TLS.ECHDoHURL, d.Config.TLS.ECHPublicName)
			cancel()

			if err == nil && len(configs) > 0 {
				echConfigList = configs
				// fmt.Println("[ECH] Config fetched successfully")
			} else {
				fmt.Printf("[ECH] Warning: Fetch failed for %s: %v. Fallback to standard TLS.\n", d.Config.TLS.ECHPublicName, err)
			}
		}

		// [Step 2] 构建 uTLS 配置
		uTlsConfig := &utls.Config{
			ServerName:         d.Config.TLS.ServerName,
			InsecureSkipVerify: d.Config.TLS.Insecure,
			MinVersion:         tls.VersionTLS12,
			// 填入解析到的 ECH 密钥 (如果为空，uTLS 会自动忽略)
			EncryptedClientHelloConfigList: echConfigList,
		}

		if uTlsConfig.ServerName == "" {
			uTlsConfig.ServerName = d.Config.Server
		}

		// [Step 3] 处理分片 (Fragment) 与握手
		var uConn *utls.UConn
		if d.Config.Settings.Fragment {
			// 启用分片，底层连接包裹 FragmentConn
			fragmentConn := &FragmentConn{Conn: conn, active: true}
			// HelloChrome_Auto 模拟 Chrome 指纹
			uConn = utls.UClient(fragmentConn, uTlsConfig, utls.HelloChrome_Auto)
		} else {
			uConn = utls.UClient(conn, uTlsConfig, utls.HelloChrome_Auto)
		}

		// 执行握手 (uTLS 会自动处理 ECH 扩展注入)
		if err := uConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("utls handshake failed: %v", err)
		}
		conn = uConn
	}

	// [Step 4] WebSocket 处理
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

// resolveECHConfig 使用 miekg/dns 解析 DoH 响应并提取 ECH 配置
func resolveECHConfig(ctx context.Context, dohURL string, domain string) ([]byte, error) {
	// 1. 构造 DNS 查询 (Type 65 - HTTPS)
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeHTTPS)
	
	// 转换为 wire format
	data, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	// 2. 发送 DoH 请求
	req, err := http.NewRequestWithContext(ctx, "POST", dohURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	// 设置标准 DoH Header
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DoH status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 3. 解析 DNS 响应
	respMsg := new(dns.Msg)
	if err := respMsg.Unpack(body); err != nil {
		return nil, err
	}

	// 4. 遍历 Answer 提取 ECH
	for _, ans := range respMsg.Answer {
		if https, ok := ans.(*dns.HTTPS); ok {
			for _, val := range https.Value {
				// miekg/dns 库将 Key=5 解析为 SVCBECH 类型
				if ech, ok := val.(*dns.SVCBECH); ok {
					return ech.Config, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no ECH config found")
}

// FragmentConn 用于在 TLS 握手初期拆分数据包
type FragmentConn struct {
	net.Conn
	active bool
}

func (f *FragmentConn) Write(b []byte) (int, error) {
	// 0x16 是 TLS Handshake 记录头的标志
	if f.active && len(b) > 50 && b[0] == 0x16 {
		f.active = false
		// 随机切分位置
		cut := 5 + rand.Intn(10)
		n1, err := f.Conn.Write(b[:cut])
		if err != nil {
			return n1, err
		}
		// 短暂睡眠增加混淆效果
		time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
		n2, err := f.Conn.Write(b[cut:])
		return n1 + n2, err
	}
	return f.Conn.Write(b)
}

// handshakeWebSocket 执行 WebSocket 握手
func (d *Dialer) handshakeWebSocket(conn net.Conn) (net.Conn, error) {
	path := d.Config.Transport.Path
	if path == "" {
		path = "/"
	}
	host := d.Config.TLS.ServerName
	if host == "" {
		host = d.Config.Server
	}

	key := make([]byte, 16)
	rand.Read(key)
	keyStr := base64.StdEncoding.EncodeToString(key)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
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
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 101 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return NewWSConn(conn, br), nil
}

// WSConn 封装 WebSocket 数据帧的读写
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
	if length == 0 {
		return 0, nil
	}

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

	// 客户端发送必须掩码处理
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
		if w.remaining > 0 {
			limit := int64(len(b))
			if w.remaining < limit {
				limit = w.remaining
			}
			n, err := w.reader.Read(b[:limit])
			if n > 0 {
				w.remaining -= int64(n)
			}
			if n > 0 || err != nil {
				return n, err
			}
		}

		header, err := w.reader.ReadByte()
		if err != nil {
			return 0, err
		}

		opcode := header & 0x0F
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

		if masked {
			// 如果服务器发来掩码数据（不常见），读取掩码并丢弃
			if _, err := io.CopyN(io.Discard, w.reader, 4); err != nil {
				return 0, err
			}
		}

		switch opcode {
		case 0x8: // Close Frame
			return 0, io.EOF
		case 0x9, 0xA: // Ping/Pong
			if payloadLen > 0 {
				io.CopyN(io.Discard, w.reader, payloadLen)
			}
			continue
		case 0x0, 0x1, 0x2: // Continuation, Text, Binary
			w.remaining = payloadLen
			if w.remaining == 0 {
				continue
			}
		default:
			if payloadLen > 0 {
				io.CopyN(io.Discard, w.reader, payloadLen)
			}
			continue
		}
	}
}
