package mobile

import (
	"fmt"
	"mandala/core/proxy"
)

// Start 启动代理服务
// localPort: Android 本地监听端口 (例如 10809)
// configJson: 包含节点信息的 JSON 字符串
func Start(localPort int, configJson string) string {
	// 调用核心层的启动逻辑
	err := proxy.Start(localPort, configJson)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "" // 返回空字符串表示成功
}

// Stop 停止代理服务
func Stop() {
	proxy.Stop()
}

// IsRunning 检查服务是否正在运行 (可选辅助函数)
// 注意：这需要 core/proxy 暴露相关状态，或者简单地在 Java 端管理状态
func IsRunning() bool {
	// 如果 proxy 包中有暴露状态，可以在这里返回
	// 目前核心层通过 GlobalServer 变量管理，这里可以增加一个检查
	return proxy.IsRunning()
}
