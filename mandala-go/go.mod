module mandala

go 1.20

require (
	golang.org/x/sys v0.15.0
	golang.org/x/time v0.5.0
	// [关键修复] 强制锁定 gVisor 到 2023.12.02 的版本
	// 这个版本与你的 stack.go/udp_nat.go 代码完全兼容
	gvisor.dev/gvisor v0.0.0-20231202080848-1f48d6a80442
)
