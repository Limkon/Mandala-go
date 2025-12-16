package proxy

import (
	"fmt"
	"net"
	"sync"

	"mandala/core/config"
)

// Server 本地代理服务器
type Server struct {
	listener net.Listener
	config   *config.OutboundConfig
	running  bool
	mu       sync.Mutex
}

var GlobalServer *Server

// Start 启动本地 SOCKS5 服务器
// localPort: Android 本地监听端口 (如 10809)
// jsonConfig: 节点配置 JSON
func Start(localPort int, jsonConfig string) error {
	Stop() // 停止旧实例

	cfg, err := config.ParseConfig(jsonConfig)
	if err != nil {
		return err
	}

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return err
	}

	srv := &Server{
		listener: l,
		config:   cfg,
		running:  true,
	}
	GlobalServer = srv

	go srv.serve()
	return nil
}

// Stop 停止服务
func Stop() {
	if GlobalServer != nil {
		GlobalServer.mu.Lock()
		defer GlobalServer.mu.Unlock()
		if GlobalServer.running {
			GlobalServer.running = false
			if GlobalServer.listener != nil {
				GlobalServer.listener.Close()
			}
		}
		GlobalServer = nil
	}
}

func (s *Server) serve() {
	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running {
				fmt.Printf("Accept error: %v\n", err)
			}
			return
		}
		
		handler := &Handler{Config: s.config}
		go handler.HandleConnection(conn)
	}
}
// core/proxy/server.go 追加内容:
func IsRunning() bool {
	if GlobalServer == nil {
		return false
	}
	GlobalServer.mu.Lock()
	defer GlobalServer.mu.Unlock()
	return GlobalServer.running
}
