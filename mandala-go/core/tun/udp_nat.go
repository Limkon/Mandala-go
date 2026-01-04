package tun

import (
	"fmt"
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

type UDPSession struct {
	LocalConn  *gonet.UDPConn
	RemoteConn net.Conn
	LastActive time.Time
	// [修复] 添加同步机制
	ready   chan struct{} // 用于通知初始化完成
	initErr error         // 存储初始化过程中的错误
}

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

func (m *UDPNatManager) GetOrCreate(key string, localConn *gonet.UDPConn, targetIP string, targetPort int) (*UDPSession, error) {
	// [修复] 构造一个新的 Session 对象，包含一个未关闭的 ready 通道
	newSession := &UDPSession{
		LocalConn:  localConn,
		LastActive: time.Now(),
		ready:      make(chan struct{}),
	}

	// [修复] 使用 LoadOrStore 原子操作
	// 如果 key 已存在，actual 返回旧值，loaded 为 true
	// 如果 key 不存在，actual 返回 newSession，loaded 为 false（我们成为了 Leader）
	actual, loaded := m.sessions.LoadOrStore(key, newSession)

	if loaded {
		existing := actual.(*UDPSession)
		
		// 等待初始化完成（如果正在初始化）
		select {
		case <-existing.ready:
			// 初始化已完成
		case <-time.After(5 * time.Second):
			// 防止极端情况下死锁
			return nil, fmt.Errorf("wait for udp session init timeout")
		}

		// 检查初始化结果
		if existing.initErr != nil {
			return nil, existing.initErr
		}

		// 检查 Stale (LocalConn 变更)
		if existing.LocalConn != localConn {
			log.Printf("GoLog: [NAT] Session stale for %s", key)
			if existing.RemoteConn != nil {
				existing.RemoteConn.Close()
			}
			m.sessions.Delete(key)
			return nil, fmt.Errorf("session stale")
		}

		existing.LastActive = time.Now()
		return existing, nil
	}

	// --- Leader 逻辑：负责执行拨号 ---
	// 无论成功失败，最后都要关闭 ready 通道以唤醒等待者
	
	// 定义清理函数，处理失败情况
	fail := func(err error) (*UDPSession, error) {
		newSession.initErr = err
		close(newSession.ready)
		m.sessions.Delete(key) // 移除占位符
		return nil, err
	}

	remoteConn, err := m.dialer.Dial()
	if err != nil {
		return fail(err)
	}

	var payload []byte
	var hErr error
	isVless := false

	switch strings.ToLower(m.config.Type) {
	case "mandala":
		client := protocol.NewMandalaClient(m.config.Username, m.config.Password)
		payload, hErr = client.BuildHandshakePayload(targetIP, targetPort, m.config.Settings.Noise)
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
		return fail(hErr)
	}

	if len(payload) > 0 {
		if _, err := remoteConn.Write(payload); err != nil {
			remoteConn.Close()
			return fail(err)
		}
	}

	if isVless {
		remoteConn = protocol.NewVlessConn(remoteConn)
	}

	// 初始化成功
	newSession.RemoteConn = remoteConn
	close(newSession.ready) // 通知等待者
	
	go m.copyRemoteToLocal(key, newSession)
	log.Printf("GoLog: [NAT] Created UDP session for %s", key)
	return newSession, nil
}

func (m *UDPNatManager) copyRemoteToLocal(key string, s *UDPSession) {
	defer func() {
		if s.RemoteConn != nil {
			s.RemoteConn.Close()
		}
		m.sessions.Delete(key)
	}()
	
	buf := make([]byte, 4096)
	for {
		s.RemoteConn.SetReadDeadline(time.Now().Add(udpTimeout))
		n, err := s.RemoteConn.Read(buf)
		if err != nil {
			return
		}
		s.LastActive = time.Now()
		if _, err := s.LocalConn.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (m *UDPNatManager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		now := time.Now()
		m.sessions.Range(func(key, value interface{}) bool {
			session := value.(*UDPSession)
			
			// [修复] 检查是否初始化完成
			select {
			case <-session.ready:
				// 如果初始化失败（RemoteConn 为 nil），或者超时
				if session.RemoteConn == nil {
					m.sessions.Delete(key)
					return true
				}
				
				if now.Sub(session.LastActive) > udpTimeout {
					session.RemoteConn.Close()
					m.sessions.Delete(key)
				}
			default:
				// 正在初始化中，跳过清理
			}
			return true
		})
	}
}
