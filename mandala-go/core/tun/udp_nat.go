package tun

import (
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"mandala/core/config"
	"mandala/core/protocol"
	"mandala/core/proxy"

	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
)

const udpTimeout = 60 * time.Second

// UDPSession 代表一个活跃的 UDP 转换会话
type UDPSession struct {
	LocalConn  *gonet.UDPConn
	RemoteConn net.Conn
	LastActive time.Time
}

// UDPNatManager 管理 UDP 会话
type UDPNatManager struct {
	sessions sync.Map
	dialer   *proxy.Dialer
	config   *config.OutboundConfig
}

func NewUDPNatManager(dialer *proxy.Dialer, cfg *config.OutboundConfig) *UDPNatManager {
	m := &UDPNatManager{
		dialer: dialer,
		config: cfg,
	}
	go m.cleanupLoop()
	return m
}

// GetOrCreate 获取现有的会话或创建新会话
func (m *UDPNatManager) GetOrCreate(key string, localConn *gonet.UDPConn, targetIP string, targetPort int) (*UDPSession, error) {
	// 1. 尝试查找现有会话
	if val, ok := m.sessions.Load(key); ok {
		session := val.(*UDPSession)
		// 如果本地连接对象变了（端口复用等情况），关闭旧的
		if session.LocalConn != localConn {
			session.RemoteConn.Close()
			m.sessions.Delete(key)
		} else {
			session.LastActive = time.Now()
			return session, nil
		}
	}

	// 2. 创建新连接
	remoteConn, err := m.dialer.Dial()
	if err != nil {
		return nil, err
	}

	// 3. 协议握手
	var payload []byte
	var hErr error
	isVless := false

	switch strings.ToLower(m.config.Type) {
	case "mandala":
		client := protocol.NewMandalaClient(m.config.Username, m.config.Password)
		
		// [修改] 获取随机填充配置
		noiseSize := 0
		if m.config.Settings != nil && m.config.Settings.Noise {
			noiseSize = m.config.Settings.NoiseSize
		}

		// [修改] 传入 noiseSize
		payload, hErr = client.BuildHandshakePayload(targetIP, targetPort, noiseSize)

	case "trojan":
		payload, hErr = protocol.BuildTrojanPayload(m.config.Password, targetIP, targetPort)
	case "vless":
		payload, hErr = protocol.BuildVlessPayload(m.config.UUID, targetIP, targetPort)
		isVless = true
	case "shadowsocks":
		payload, hErr = protocol.BuildShadowsocksPayload(targetIP, targetPort)
	case "socks", "socks5":
		hErr = protocol.HandshakeSocks5(remoteConn, m.config.Username, m.config.Password, targetIP, targetPort)
	}

	if hErr != nil {
		remoteConn.Close()
		return nil, hErr
	}

	// 发送握手包
	if len(payload) > 0 {
		if _, err := remoteConn.Write(payload); err != nil {
			remoteConn.Close()
			return nil, err
		}
	}

	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	session := &UDPSession{
		LocalConn:  localConn,
		RemoteConn: remoteConn,
		LastActive: time.Now(),
	}

	m.sessions.Store(key, session)

	// 启动远程数据回传协程
	go m.copyRemoteToLocal(key, session)

	log.Printf("GoLog: [NAT] 创建 UDP 会话: %s", key)
	return session, nil
}

// copyRemoteToLocal 从远程代理读取数据并写回本地 UDP 连接
func (m *UDPNatManager) copyRemoteToLocal(key string, session *UDPSession) {
	defer func() {
		session.RemoteConn.Close()
		m.sessions.Delete(key)
	}()

	buf := make([]byte, 4096)
	for {
		// 设置读取超时，实现空闲断开
		session.RemoteConn.SetReadDeadline(time.Now().Add(udpTimeout))
		n, err := session.RemoteConn.Read(buf)
		if err != nil {
			if err != io.EOF {
				// log.Printf("UDP Read error: %v", err)
			}
			return
		}

		session.LastActive = time.Now()
		
		// 写回 TUN
		if _, err := session.LocalConn.Write(buf[:n]); err != nil {
			return
		}
	}
}

// cleanupLoop 定期清理过期会话
func (m *UDPNatManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		m.sessions.Range(func(key, value interface{}) bool {
			session := value.(*UDPSession)
			if now.Sub(session.LastActive) > udpTimeout {
				session.RemoteConn.Close()
				m.sessions.Delete(key)
				// log.Printf("GoLog: [NAT] 清理过期会话: %v", key)
			}
			return true
		})
	}
}
