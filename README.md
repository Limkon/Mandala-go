Project-Root/
├── .github/
│   └── workflows/
│       └── build.yml                # [CI] GitHub Actions 编译脚本
│
├── mandala-go/                      # [Core] Go 语言核心代码目录
│   ├── go.mod                       # Go 模块定义
│   ├── core/                        # 核心业务逻辑
│   │   ├── config/                  # 配置解析
│   │   ├── protocol/                # 协议实现 (Mandala/Vless 等)
│   │   └── proxy/                   # 代理服务器与流量转发
│   └── mobile/                      # Gomobile 接口层
│       └── lib.go                   # 暴露给 Android 的 Start/Stop 接口
│
└── android/                         # [UI] Android 原生项目目录
    ├── build.gradle.kts             # 项目级构建配置
    ├── settings.gradle.kts          # 项目设置
    ├── gradle.properties
    ├── gradlew                      # Gradle Wrapper
    │
    └── app/                         # App 主模块
        ├── build.gradle.kts         # 模块级构建配置
        ├── libs/                    # 存放生成的 mandala.aar
        └── src/
            └── main/
                ├── AndroidManifest.xml
                ├── res/             # 资源文件 (图标, 布局, 字符串)
                │   ├── drawable/
                │   ├── mipmap/
                │   └── values/
                └── java/
                    └── com/
                        └── example/
                            └── mandala/
                                ├── MainActivity.kt          # 主入口
                                ├── MandalaApplication.kt    # 全局 Application
                                ├── viewmodel/               # MVVM ViewModel
                                │   └── MainViewModel.kt
                                └── ui/                      # Compose UI 组件
                                    ├── theme/
                                    ├── home/
                                    ├── profiles/
                                    └── settings/
