package mobile

import (
	"fmt"
	"mandala/core/config"
	"mandala/core/proxy"
	"mandala/core/tun"
	"sync"
)

var (
	vpnStack *tun.Stack
	mu       sync.Mutex
)

// Start 启动本地 SOCKS5 服务器 (旧模式，保留兼容性)
func Start(localPort int, jsonConfig string) string {
	err := proxy.Start(localPort, jsonConfig)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return ""
}

// StartVpn 启动 VPN 模式 (新模式)
// fd: Android 传递过来的 TUN 文件描述符。
// [修复] mtu 改为 int32，确保在 Java 侧映射为 int，避免 32/64 位系统下的类型歧义。
func StartVpn(fd int32, mtu int32, jsonConfig string) string {
	mu.Lock()
	defer mu.Unlock()

	// 如果已有实例在运行，先停止
	if vpnStack != nil {
		vpnStack.Close()
		vpnStack = nil
	}

	// 解析配置
	cfg, err := config.ParseConfig(jsonConfig)
	if err != nil {
		return fmt.Sprintf("Config Error: %v", err)
	}

	// 启动网络栈
	// 注意：tun.StartStack 接收 int，这里将 int32 转换为 int
	stack, err := tun.StartStack(int(fd), int(mtu), cfg)
	if err != nil {
		return fmt.Sprintf("Stack Error: %v", err)
	}

	vpnStack = stack
	return ""
}

// Stop 停止服务 (同时支持两种模式)
func Stop() {
	// 停止 SOCKS5
	proxy.Stop()

	// 停止 VPN
	mu.Lock()
	if vpnStack != nil {
		vpnStack.Close()
		vpnStack = nil
	}
	mu.Unlock()
}

// IsRunning 检查服务状态
func IsRunning() bool {
	// 简单检查 SOCKS 或 VPN 任意一个在运行即可
	if proxy.IsRunning() {
		return true
	}
	mu.Lock()
	defer mu.Unlock()
	return vpnStack != nil
}
