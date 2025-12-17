module mandala

go 1.20

require (
	golang.org/x/sys v0.15.0
	golang.org/x/time v0.5.0
	golang.org/x/net v0.19.0
	// 项目代码依赖较新的 gVisor API（如 gonet.NewUDPConn 需要 *stack.Stack 参数、tcpip.Error 有 IgnoreStats 等）
	// 同时必须避开最新 master 的测试文件包冲突问题
	// 推荐使用 2024 年 4 月初的 commit：这个时间点已包含 Subnet 等新 API，但尚未引入导致 gomobile 报 “found packages stack and bridge” 的测试文件
	gvisor.dev/gvisor v0.0.0-20240320201045-8e071b99f562
)

// 移除 replace 指令（或注释掉），让 Go 直接使用上面指定的有效版本
// replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20240408034247-4148b874457e
