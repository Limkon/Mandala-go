package proxy

import (
	"fmt"
	"net"
	"time"

	"mandala/core/config"
)

// Dialer 负责建立到代理服务器的连接
type Dialer struct {
	config *config.OutboundConfig
}

// NewDialer 创建一个新的拨号器
func NewDialer(cfg *config.OutboundConfig) *Dialer {
	return &Dialer{
		config: cfg,
	}
}

// Dial 连接到配置文件中指定的远程服务器
func (d *Dialer) Dial() (net.Conn, error) {
	// 拼接地址 server:port
	addr := fmt.Sprintf("%s:%d", d.config.Server, d.config.ServerPort)
	
	// 建立 TCP 连接，设置 10 秒超时
	return net.DialTimeout("tcp", addr, 10*time.Second)
}
