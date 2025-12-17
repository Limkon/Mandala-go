module mandala

go 1.20

require (
	golang.org/x/mobile v0.0.0-20231127183840-76ac6878050a
	golang.org/x/sys v0.15.0
	golang.org/x/time v0.5.0
	gvisor.dev/gvisor v0.0.0-20231023213702-2691a8f9b1cf
)

// [关键修复] 强制替换为 2023-10-23 的真实版本
// 这个版本包含 Mask 字段 API，且真实存在于代理服务器中
replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20231023213702-2691a8f9b1cf
