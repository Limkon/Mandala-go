module mandala

go 1.20

require (
	golang.org/x/mobile v0.0.0-20231127183840-76ac6878050a // 显式引入 mobile 库
	golang.org/x/sys v0.15.0
	golang.org/x/time v0.5.0
	// 强制锁定到 2023.12 的稳定版
	gvisor.dev/gvisor v0.0.0-20231202080848-1f48d6a80442
)

// 双重保险：强制替换
replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20231202080848-1f48d6a80442
