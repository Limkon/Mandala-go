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

// ECH 缓存
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
		var echConfigList []byte
		
		// [ECH 逻辑]
		if d.Config.TLS.EnableECH {
			queryDomain := d.Config.TLS.ECHPublicName
			if queryDomain == "" {
				queryDomain = d.Config.TLS.ServerName
			}
			
			echCacheMutex.RLock()
			cached, ok := echCache[queryDomain]
			echCacheMutex.RUnlock()

			if ok {
				echConfigList = cached
				fmt.Printf("[ECH] 使用缓存密钥: %s\n", queryDomain)
			} else {
				dohURL := d.Config.TLS.ECHDoHURL
				if dohURL == "" {
					dohURL = "https://1.1.1.1/dns-query"
				}

				// fmt.Printf("[ECH] 查询密钥: %s -> %s\n", dohURL, queryDomain)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) 
				configs, err := resolveECHConfig(ctx, dohURL, queryDomain)
				cancel()

				if err == nil && len(configs) > 0 {
					echConfigList = configs
					echCacheMutex.Lock()
					echCache[queryDomain] = configs
					echCacheMutex.Unlock()
					fmt.Printf("[ECH] 密钥获取成功\n")
				} else {
					fmt.Printf("[ECH] 警告: 密钥获取失败: %v\n", err)
				}
			}
		}

		// 判断是否启用了 ECH
		useECH := len(echConfigList) > 0

		// [TLS 1.3 强制] 如果使用 ECH，必须强制 TLS 1.3，否则使用默认 (TLS 1.2)
		minVer := uint16(tls.VersionTLS12)
		if useECH {
			minVer = tls.VersionTLS13
		}

		uTlsConfig := &utls.Config{
			ServerName:         d.Config.TLS.ServerName,
			InsecureSkipVerify: d.Config.TLS.Insecure,
			MinVersion:         minVer,
			// 注意：这里虽设置了 NextProtos，但 utls 的 ClientHello 最终由 Spec 决定
			// 为了保持一致性，如果不开 ECH，我们允许 h2
			NextProtos:         []string{"h2", "http/1.1"}, 
			EncryptedClientHelloConfigList: echConfigList,
		}

		if useECH {
			// 如果开启 ECH，配置中仅保留 http/1.1 提示
			uTlsConfig.NextProtos = []string{"http/1.1"}
		}

		if uTlsConfig.ServerName == "" {
			uTlsConfig.ServerName = d.Config.Server
		}

		if d.Config.Settings.Fragment {
			conn = &FragmentConn{Conn: conn, active: true}
		}

		// 使用 HelloCustom 以便我们能灵活修改指纹
		uConn := utls.UClient(conn, uTlsConfig, utls.HelloCustom)

		// 1. 加载 Chrome 模版指纹
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to get uTLS spec: %v", err)
		}

		// 2. [差异化逻辑] 根据 ECH 状态调整 ALPN
		if useECH {
			// 【开启 ECH 时】：强制只使用 http/1.1
			// 原因：防止 Cloudflare 协商到 h2 导致 WebSocket 库崩溃
			foundALPN := false
			for i, ext := range spec.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"} // 强制覆盖
					spec.Extensions[i] = alpn
					foundALPN = true
					break
				}
			}
			if !foundALPN {
				spec.Extensions = append(spec.Extensions, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
			}
		} else {
			// 【未开启 ECH 时】：不修改指纹，使用自动协商 (保留 h2)
			// 此时 spec 中的 ALPN 依然包含 "h2", "http/1.1"
			// 注意：如果服务端协商出 h2，WebSocket 可能会报错 "malformed HTTP response"
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

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

// resolveECHConfig
func resolveECHConfig(ctx context.Context, dohURL string, domain string) ([]byte, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeHTTPS)
	data, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack: %v", err)
	}

	b64Query := base64.RawURLEncoding.EncodeToString(data)
	
	var reqURL string
	if strings.Contains(dohURL, "?") {
		reqURL = fmt.Sprintf("%s&dns=%s", dohURL, b64Query)
	} else {
		reqURL = fmt.Sprintf("%s?dns=%s", dohURL, b64Query)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-message")

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			ResponseHeaderTimeout: 5 * time.Second,
		},
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
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

	return nil, fmt.Errorf("no ech found")
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
