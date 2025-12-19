package mobile

import (
	"encoding/json"
	"mandala/core/config"
	"mandala/core/tun"
)

var stack *tun.Stack

// Logger 介面，用於回調 Kotlin
type Logger interface {
	OnLog(msg string)
}

var appLogger Logger

func SetLogger(l Logger) {
	appLogger = l
}

func StartVpn(fd int, mtu int, configJson string) string {
	if stack != nil {
		return "VPN already running"
	}

	var cfg config.OutboundConfig
	if err := json.Unmarshal([]byte(configJson), &cfg); err != nil {
		return err.Error()
	}

	s, err := tun.StartStack(fd, mtu, &cfg)
	if err != nil {
		return err.Error()
	}

	stack = s
	if appLogger != nil {
		appLogger.OnLog("Go Core Started Successfully")
	}
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
