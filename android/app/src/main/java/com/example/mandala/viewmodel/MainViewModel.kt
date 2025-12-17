package com.example.mandala.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.example.mandala.utils.NodeParser
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import mobile.Mobile // 引用 Gomobile 生成的库

// 定义节点数据结构
data class Node(
    val tag: String,
    val protocol: String, // "mandala", "vless", "trojan"
    val server: String,
    val port: Int,
    val password: String = "",
    val uuid: String = "",
    val transport: String = "tcp", // "ws", "tcp"
    val isSelected: Boolean = false
)

class MainViewModel : ViewModel() {
    // --- UI 状态流 ---
    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _connectionTime = MutableStateFlow("00:00:00")
    val connectionTime = _connectionTime.asStateFlow()

    // 当前选中的节点
    private val _currentNode = MutableStateFlow(
        Node("HK - Mandala VIP", "mandala", "hk.example.com", 443, password = "your-password", transport = "ws")
    )
    val currentNode = _currentNode.asStateFlow()

    // 日志流
    private val _logs = MutableStateFlow(listOf("[系统] 就绪"))
    val logs = _logs.asStateFlow()

    // 节点列表
    private val _nodes = MutableStateFlow(listOf(
        Node("HK - Mandala VIP", "mandala", "hk.example.com", 443, transport = "ws"),
        Node("JP - Trojan Fast", "trojan", "jp.example.com", 443),
        Node("US - VLESS Direct", "vless", "us.example.com", 80)
    ))
    val nodes = _nodes.asStateFlow()

    // --- 核心操作 ---

    fun toggleConnection() {
        if (_isConnected.value) {
            stopProxy()
        } else {
            startProxy()
        }
    }

    private fun startProxy() {
        viewModelScope.launch {
            try {
                addLog("[核心] 正在准备配置...")
                val configJson = generateConfigJson(_currentNode.value)

                // 调用 Go 核心启动函数 (监听本地 10809)
                addLog("[核心] 正在启动服务 (端口 10809)...")
                val error = Mobile.start(10809, configJson)

                if (error.isEmpty()) {
                    _isConnected.value = true
                    addLog("[核心] 服务启动成功")
                    // 这里可以启动一个计时器协程来更新 _connectionTime
                } else {
                    addLog("[错误] 启动失败: $error")
                    _isConnected.value = false
                }
            } catch (e: Exception) {
                addLog("[异常] ${e.message}")
            }
        }
    }

    private fun stopProxy() {
        viewModelScope.launch {
            try {
                addLog("[核心] 正在停止服务...")
                Mobile.stop()
                _isConnected.value = false
                addLog("[核心] 服务已停止")
            } catch (e: Exception) {
                addLog("[异常] 停止失败: ${e.message}")
            }
        }
    }

    fun selectNode(node: Node) {
        // 如果正在运行，先停止
        if (_isConnected.value) {
            stopProxy()
        }
        _currentNode.value = node
        addLog("[系统] 切换到节点: ${node.tag}")
    }

    /**
     * 添加新节点到列表
     * 如果存在相同 server 和 tag 的节点则更新，否则插入到头部
     */
    fun addNode(node: Node) {
        val currentList = _nodes.value.toMutableList()
        val index = currentList.indexOfFirst { it.tag == node.tag && it.server == node.server }
        
        if (index != -1) {
            currentList[index] = node
            addLog("[系统] 更新节点: ${node.tag}")
        } else {
            currentList.add(0, node)
            addLog("[系统] 添加新节点: ${node.tag}")
        }
        _nodes.value = currentList
    }

    /**
     * 从文本导入节点 (支持 vmess/trojan/mandala 链接)
     * 返回 true 表示导入成功
     */
    fun importFromText(text: String): Boolean {
        val node = NodeParser.parse(text)
        return if (node != null) {
            addNode(node)
            true
        } else {
            addLog("[错误] 无法解析剪贴板内容或格式不支持")
            false
        }
    }

    private fun addLog(msg: String) {
        val currentLogs = _logs.value.toMutableList()
        if (currentLogs.size > 100) currentLogs.removeAt(0) // 保持日志长度
        currentLogs.add(msg)
        _logs.value = currentLogs
    }

    // 生成符合 Go 核心要求的 JSON 配置字符串
    private fun generateConfigJson(node: Node): String {
        // 简单的手动拼接 JSON
        return """
        {
            "tag": "${node.tag}",
            "type": "${node.protocol}",
            "server": "${node.server}",
            "server_port": ${node.port},
            "password": "${node.password}",
            "uuid": "${node.uuid}",
            "tls": {
                "enabled": true,
                "server_name": "${node.server}"
            },
            "transport": {
                "type": "${node.transport}",
                "path": "/"
            }
        }
        """.trimIndent()
    }
}
