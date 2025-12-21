package config

import (
	"encoding/json"
	"fmt"
)

// OutboundConfig 定義了單個代理節點的配置信息
type OutboundConfig struct {
	Tag        string `json:"tag"`
	Type       string `json:"type"` // 協議類型: "mandala", "vless", "trojan", "shadowsocks", "socks"
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	
	// 鑑權字段
	UUID     string `json:"uuid,omitempty"`     // VLESS/VMess 使用
	Password string `json:"password,omitempty"` // Mandala/Trojan/Shadowsocks 使用
	Username string `json:"username,omitempty"` // SOCKS5 使用

	// 高級配置
	TLS       *TLSConfig       `json:"tls,omitempty"`
	Transport *TransportConfig `json:"transport,omitempty"`
}

// TLSConfig 定義 TLS 相關配置
type TLSConfig struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name,omitempty"` // SNI
	Insecure   bool   `json:"insecure,omitempty"`    // 是否跳過證書驗證
}

// TransportConfig 定義傳輸層配置 (如 WebSocket)
type TransportConfig struct {
	Type    string            `json:"type"` // "ws" 等
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Config 是傳遞給核心啟動函數的總配置結構
type Config struct {
	CurrentNode *OutboundConfig `json:"current_node"`
	LocalPort   int             `json:"local_port"`
	Debug       bool            `json:"debug"`
}

// ParseConfig 解析 JSON 字符串為配置對象
func ParseConfig(jsonStr string) (*OutboundConfig, error) {
	var cfg OutboundConfig
	err := json.Unmarshal([]byte(jsonStr), &cfg)
	if err != nil {
		return nil, fmt.Errorf("config parse error: %v", err)
	}

	// [修復] 兼容性處理：如果 Username 為空但 UUID 有值（常見於從 Socks5 鏈接導入的節點）
	// 確保在握手時能讀取到正確的認證信息
	if (cfg.Type == "socks" || cfg.Type == "socks5") && cfg.Username == "" && cfg.UUID != "" {
		cfg.Username = cfg.UUID
	}

	return &cfg, nil
}
