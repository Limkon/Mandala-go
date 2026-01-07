package proxy

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"mandala/core/config"

	"github.com/coder/websocket"
	"github.com/miekg/dns"
	utls "github.com/refraction-networking/utls"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Dialer struct {
	Config *config.OutboundConfig
}

func NewDialer(cfg *config.OutboundConfig) *Dialer {
	return &Dialer{Config: cfg}
}

// Dial 建立连接
func (d *Dialer) Dial() (net.Conn, error) {
	// 1. 基础 TCP 连接
	targetAddr := fmt.Sprintf("%s:%d", d.Config.Server, d.Config.ServerPort)
	conn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}

	// 2. 准备 TLS/uTLS 连接
	isTLSEstablished := false
	
	if d.Config.TLS != nil && d.Config.TLS.Enabled {
		// [ECH] 获取配置
		var echConfigList []byte
		if d.Config.TLS.EnableECH && d.Config.TLS.ECHDoHURL != "" && d.Config.TLS.ECHPublicName != "" {
			// 使用较短超时，避免阻塞
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			configs, err := resolveECHConfig(ctx, d.Config.TLS.ECHDoHURL, d.Config.TLS.ECHPublicName)
			cancel()

			if err == nil && len(configs) > 0 {
				echConfigList = configs
				// fmt.Printf("[ECH] Config fetched for %s\n", d.Config.TLS.ECHPublicName)
			} else {
				fmt.Printf("[ECH] Warning: Fetch failed: %v. Fallback to standard TLS.\n", err)
			}
		}

		uTlsConfig := &utls.Config{
			ServerName:         d.Config.TLS.ServerName,
			InsecureSkipVerify: d.Config.TLS.Insecure,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"http/1.1"},
			EncryptedClientHelloConfigList: echConfigList, // 设置 ECH 配置
		}

		if uTlsConfig.ServerName == "" {
			uTlsConfig.ServerName = d.Config.Server
		}

		// 处理 TCP 层分片
		if d.Config.Settings.Fragment {
			conn = &FragmentConn{Conn: conn, active: true}
		}

		uConn := utls.UClient(conn, uTlsConfig, utls.HelloCustom)

		// 1. 获取 Chrome 指纹 (HelloChrome_Auto 会自动匹配最新版 Chrome 指纹)
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to get uTLS spec: %v", err)
		}

		// 2. 强制 ALPN 为 http/1.1
		foundALPN := false
		for i, ext := range spec.Extensions {
			if alpn, ok := ext.(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
				spec.Extensions[i] = alpn
				foundALPN = true
				break
			}
		}
		if !foundALPN {
			spec.Extensions = append(spec.Extensions, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
		}

		// [修正] 移除导致编译错误的手动注入代码
		// utls 库在使用 Config.EncryptedClientHelloConfigList 时，
		// 会根据 ClientHelloSpec 自动处理 ECH 扩展。
		// 新版 utls 中 EncryptedClientHelloExtension 是接口，无法直接实例化。

		if err := uConn.ApplyPreset(&spec); err != nil {
			conn.Close()
			return nil, fmt.Errorf("apply preset failed: %v", err)
		}

		if err := uConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("utls handshake failed: %v", err)
		}
		
		conn = uConn
		isTLSEstablished = true
	}

	// 3. 处理 WebSocket
	if d.Config.Transport != nil && d.Config.Transport.Type == "ws" {
		scheme := "ws"
		if d.Config.TLS != nil && d.Config.TLS.Enabled && !isTLSEstablished {
			scheme = "wss"
		}
		
		path := d.Config.Transport.Path
		if path == "" {
			path = "/"
		}
		
		host := d.Config.TLS.ServerName
		if host == "" {
			host = d.Config.Server
		}
		
		wsURL := fmt.Sprintf("%s://%s%s", scheme, host, path)

		headers := make(http.Header)
		if d.Config.Transport.Headers != nil {
			for k, v := range d.Config.Transport.Headers {
				headers.Set(k, v)
			}
		}
		headers.Set("Host", host)

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return conn, nil
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		opts := &websocket.DialOptions{
			HTTPClient: httpClient,
			HTTPHeader: headers,
			CompressionMode: websocket.CompressionDisabled,
		}

		wsConn, _, err := websocket.Dial(ctx, wsURL, opts)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("websocket dial failed: %v", err)
		}

		return websocket.NetConn(context.Background(), wsConn, websocket.MessageBinary), nil
	}

	return conn, nil
}

// resolveECHConfig 通过 DoH 查询 HTTPS 记录并提取 ECH 配置
// [优化] 使用 GET 请求 (Base64Url 编码 DNS 报文)，兼容性更好
func resolveECHConfig(ctx context.Context, dohURL string, domain string) ([]byte, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeHTTPS)

	data, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	// 使用 Base64Url 编码，构造 ?dns=... 参数
	b64Query := base64.RawURLEncoding.EncodeToString(data)
	reqURL := fmt.Sprintf("%s?dns=%s", dohURL, b64Query)
	
	// 处理 dohURL 原本可能包含参数的情况
	if strings.Contains(dohURL, "?") {
		// 简单的追加逻辑，实际场景可能需要更严谨的 URL 解析
		if strings.HasSuffix(dohURL, "?") {
			reqURL = fmt.Sprintf("%sdns=%s", dohURL, b64Query)
		} else {
			reqURL = fmt.Sprintf("%s&dns=%s", dohURL, b64Query)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	// 设置 DoH 标准头
	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("Content-Type", "application/dns-message")

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			ResponseHeaderTimeout: 3 * time.Second,
		},
	}
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

	respMsg := new(dns.Msg)
	if err := respMsg.Unpack(body); err != nil {
		return nil, err
	}

	for _, ans := range respMsg.Answer {
		if https, ok := ans.(*dns.HTTPS); ok {
			for _, val := range https.Value {
				if ech, ok := val.(*dns.SVCBECHConfig); ok {
					return ech.ECH, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no ECH config found")
}

// FragmentConn 保持不变
type FragmentConn struct {
	net.Conn
	active bool
}

func (f *FragmentConn) Write(b []byte) (int, error) {
	if f.active && len(b) > 50 && b[0] == 0x16 {
		f.active = false
		cut := 5 + rand.Intn(10)
		n1, err := f.Conn.Write(b[:cut])
		if err != nil {
			return n1, err
		}
		time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
		n2, err := f.Conn.Write(b[cut:])
		return n1 + n2, err
	}
	return f.Conn.Write(b)
}
