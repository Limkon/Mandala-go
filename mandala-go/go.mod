module mandala

go 1.20

require (
	// 工具依赖
	golang.org/x/mobile v0.0.0-20231127183840-76ac6878050a
	golang.org/x/mod v0.14.0 // 间接依赖
	golang.org/x/tools v0.16.0 // 间接依赖

	// [新增] 引入 miekg/dns 用于解析 ECH
	github.com/miekg/dns v1.1.62
	
	// ECH 握手库
	github.com/refraction-networking/utls v1.6.7
	
	// 移除旧的 dnsmessage 依赖，保留 net 用于其他网络功能
	golang.org/x/net v0.27.0

	// 项目依赖
	golang.org/x/sys v0.22.0
	golang.org/x/time v0.5.0
	gvisor.dev/gvisor v0.0.0-20231023213702-2691a8f9b1cf
)

// 锁定 gVisor
replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20231023213702-2691a8f9b1cf
