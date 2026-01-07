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
	"sync"
	"time"

	"mandala/core/config"

	"github.com/coder/websocket"
	"github.com/miekg/dns"
	utls "github.com/refraction-networking/utls"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// 简单的内存缓存，用于存储 ECH 配置，避免重复查询
var (
	echCache      = make(map[string][]byte)
	echCacheMutex sync.RWMutex
)

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
		
		// 检查是否启用了 ECH 且配置了 DoH
		if d.Config.TLS.EnableECH && d.Config.TLS.ECHDoHURL != "" {
			// 确定查询的目标域名：优先用 PublicName，如果没有则用 ServerName
			queryDomain := d.Config.TLS.ECHPublicName
			if queryDomain == "" {
				queryDomain = d.Config.TLS.ServerName // [自动回退]
			}

			// 尝试从缓存获取
			echCacheMutex.RLock()
			cached, ok := echCache[queryDomain]
			echCacheMutex.RUnlock()

			if ok {
				echConfigList = cached
			} else {
				// 缓存未命中，发起 DoH 查询
				// 设置较短的超时，避免阻塞主连接太久
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) 
				configs, err := resolveECHConfig(ctx, d.Config.TLS.ECHDoHURL, queryDomain)
				cancel()

				if err == nil && len(configs) > 0 {
					echConfigList = configs
					// 写入缓存 (简单策略：不设过期，直到重启)
					echCacheMutex.Lock()
					echCache[queryDomain] = configs
					echCacheMutex.Unlock()
					fmt.Printf("[ECH] Config fetched for %s\n", queryDomain)
				} else {
					fmt.Printf("[ECH] Warning: Fetch failed for %s: %v. Fallback to standard TLS.\n", queryDomain, err)
				}
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

		// 3. [关键] 强制注入 ECH 扩展
		if len(echConfigList) > 0 {
			hasECH := false
			for _, ext := range spec.Extensions {
				if _, ok := ext.(*utls.EncryptedClientHelloExtension); ok {
					hasECH = true
					break
				}
			}
			if !hasECH {
				spec.Extensions = append(spec.Extensions, &utls.EncryptedClientHelloExtension{})
			}
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

	// 3. 处理 WebSocket (保持原有修复逻辑)
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
// 改进点：支持 GET 方法 (Base64Url)，兼容性更好
func resolveECHConfig(ctx context.Context, dohURL string, domain string) ([]byte, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeHTTPS)

	// 序列化 DNS 请求
	data, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	// 使用 GET 方法发起请求 (Base64Url 编码 DNS 报文)
	// 格式: ?dns=<base64url-encoded-message>
	b64Query := base64.RawURLEncoding.EncodeToString(data)
	reqURL := fmt.Sprintf("%s?dns=%s", dohURL, b64Query)
	// 如果 dohURL 已经包含参数，需要适当处理 (简化起见假设 dohURL 不含参数或以 ? 结尾不太可能)
	if strings.Contains(dohURL, "?") {
		reqURL = fmt.Sprintf("%s&dns=%s", dohURL, b64Query)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	
	// 设置标准 DoH 头
	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("Content-Type", "application/dns-message") // GET 请求通常不需要 Content-Type，但带上无妨

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

	// 解析 HTTPS 记录中的 ECH 配置
	for _, ans := range respMsg.Answer {
		if https, ok := ans.(*dns.HTTPS); ok {
			for _, val := range https.Value {
				if ech, ok := val.(*dns.SVCBECHConfig); ok {
					return ech.ECH, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no ECH config found in DNS response")
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
