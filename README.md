mandala-go/   
├── go.mod                  # Go 模块依赖定义   
├── go.sum   
├── core/                   # [核心层] 纯 Go 实现的业务逻辑，不依赖 Android API   
│   ├── config/             # 配置解析模块   
│   │   └── config.go       # 定义 Config 结构体与 JSON 解析 (对应 config.c)   
│   ├── protocol/           # 协议实现模块    
│   │   ├── mandala.go      # Mandala 协议握手与封装 (对应 proxy.c 核心逻辑)   
│   │   ├── crypto.go       # 密码学工具 (SHA224, Base64 等，对应 crypto.c)   
│   │   └── utils.go        # 通用网络辅助函数   
│   └── proxy/              # 代理服务模块   
│       ├── server.go       # 本地 SOCKS5/HTTP 监听器 (对应 server_thread)   
│       └── handler.go      # 流量转发与连接处理 (对应 client_handler)   
└── mobile/                 # [接口层] Gomobile 绑定入口   
    └── lib.go              # 暴露给 Android (Java/Kotlin) 的 API (Start, Stop 等)   
