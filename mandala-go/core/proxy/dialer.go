package proxy

import (
	"context"
	"crypto/tls"
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
	// 标记是否已经完成了 TLS 握手
	isTLSEstablished := false
	
	if d.Config.TLS != nil && d.Config.TLS.Enabled {
		// [ECH] 获取配置
		var echConfigList []byte
		if d.Config.TLS.EnableECH && d.Config.TLS.ECHDoHURL != "" && d.Config.TLS.ECHPublicName != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // 缩短超时防止卡顿
			configs, err := resolveECHConfig(ctx, d.Config.TLS.ECHDoHURL, d.Config.TLS.ECHPublicName)
			cancel()

			if err == nil && len(configs) > 0 {
				echConfigList = configs
			} else {
				fmt.Printf("[ECH] Warning: Fetch failed: %v. Fallback to standard TLS.\n", err)
			}
		}

		uTlsConfig := &utls.Config{
			ServerName:         d.Config.TLS.ServerName,
			InsecureSkipVerify: d.Config.TLS.Insecure,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"http/1.1"},
			EncryptedClientHelloConfigList: echConfigList,
		}

		if uTlsConfig.ServerName == "" {
			uTlsConfig.ServerName = d.Config.Server
		}

		// 处理 TCP 层分片
		if d.Config.Settings.Fragment {
			conn = &FragmentConn{Conn: conn, active: true}
		}

		uConn := utls.UClient(conn, uTlsConfig, utls.HelloCustom)

		// 1. 获取 Chrome 指纹
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
		// [关键修复]
		// 如果我们已经在上方手动完成了 TLS 握手 (isTLSEstablished == true)，
		// 这里的 scheme 必须是 "ws" 而不是 "wss"。
		// 因为 conn 已经是加密连接，websocket 库只需要写入明文 HTTP Upgrade 请求即可。
		// 如果传 "wss"，库会尝试再次进行 TLS 握手，导致 "server gave HTTP response to HTTPS client" 错误。
		scheme := "ws"
		if d.Config.TLS != nil && d.Config.TLS.Enabled && !isTLSEstablished {
			// 只有在没手动处理 TLS 的情况下，才让 websocket 库处理 wss
			scheme = "wss"
		}
		
		path := d.Config.Transport.Path
		if path == "" {
			path = "/"
		}
		
		// Host 头用于服务器路由，必须正确
		host := d.Config.TLS.ServerName
		if host == "" {
			host = d.Config.Server
		}
		
		// 这里的 wsURL 只是给库解析用的，实际上数据走的是 DialContext 返回的 conn
		wsURL := fmt.Sprintf("%s://%s%s", scheme, host, path)

		headers := make(http.Header)
		if d.Config.Transport.Headers != nil {
			for k, v := range d.Config.Transport.Headers {
				headers.Set(k, v)
			}
		}
		// 确保 Host 头存在
		headers.Set("Host", host)

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					// 直接返回已经建立好的（可能是加密的）连接
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

// resolveECHConfig 保持不变
func resolveECHConfig(ctx context.Context, dohURL string, domain string) ([]byte, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeHTTPS)

	data, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", dohURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
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
