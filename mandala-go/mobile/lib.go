package mobile

import (
	"encoding/json"
	"mandala/core/config"
	"mandala/core/tun"
)

var stack *tun.Stack

// Logger 接口，用于回调 Kotlin
type Logger interface {
	OnLog(msg string)
}

var appLogger Logger

// SetLogger 设置全局日志记录器
func SetLogger(l Logger) {
	appLogger = l
}

// StartVpn 启动 VPN 核心栈
func StartVpn(fd int64, mtu int64, configJson string) string {
	if stack != nil {
		return "VPN 已经在运行中"
	}

	var cfg config.OutboundConfig
	if err := json.Unmarshal([]byte(configJson), &cfg); err != nil {
		return "配置解析失败: " + err.Error()
	}

	// 启动网络栈
	s, err := tun.StartStack(int(fd), int(mtu), &cfg)
	if err != nil {
		return "网络栈启动失败: " + err.Error()
	}

	stack = s
	if appLogger != nil {
		appLogger.OnLog("Go 核心引擎启动成功")
	}
	return ""
}

// Stop 停止 VPN 核心栈
func Stop() {
	if stack != nil {
		stack.Close()
		stack = nil
		if appLogger != nil {
			appLogger.OnLog("Go 核心引擎已关闭")
		}
	}
}

// IsRunning 检查引擎状态
func IsRunning() bool {
	return stack != nil
}
