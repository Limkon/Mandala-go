package mobile

import (
	"encoding/json"
	"mandala/core/config"
	"mandala/core/tun"
)

var stack *tun.Stack

// StartVpn 启动 VPN 核心，fd 使用 int64 以匹配 Java Long
func StartVpn(fd int64, mtu int64, configJson string) string {
	if stack != nil {
		return "VPN已经在运行"
	}

	var cfg config.OutboundConfig
	if err := json.Unmarshal([]byte(configJson), &cfg); err != nil {
		return "解析配置失败: " + err.Error()
	}

	// 转换回 int 使用
	s, err := tun.StartStack(int(fd), int(mtu), &cfg)
	if err != nil {
		return "启动核心失败: " + err.Error()
	}

	stack = s
	return ""
}

func Stop() {
	if stack != nil {
		stack.Close()
		stack = nil
	}
}

func IsRunning() bool {
	return stack != nil
}
