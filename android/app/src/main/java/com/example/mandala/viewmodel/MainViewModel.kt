package com.example.mandala.viewmodel

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.example.mandala.data.NodeRepository
import com.example.mandala.utils.NodeParser
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import mobile.Mobile

// 节点数据模型
data class Node(
    val tag: String,
    val protocol: String,
    val server: String,
    val port: Int,
    val password: String = "",
    val uuid: String = "",
    val transport: String = "tcp",
    val path: String = "/",
    val sni: String = "",
    val isSelected: Boolean = false
)

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val repository = NodeRepository(application)

    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _logs = MutableStateFlow(listOf("[系统] 准备就绪"))
    val logs = _logs.asStateFlow()

    private val _nodes = MutableStateFlow<List<Node>>(emptyList())
    val nodes = _nodes.asStateFlow()

    private val _currentNode = MutableStateFlow(Node("未选择", "none", "0.0.0.0", 0))
    val currentNode = _currentNode.asStateFlow()

    // VPN 事件通道，用于通知 Service
    sealed class VpnEvent {
        data class StartVpn(val configJson: String) : VpnEvent()
        object StopVpn : VpnEvent()
    }
    private val _vpnEventChannel = Channel<VpnEvent>()
    val vpnEvent = _vpnEventChannel.receiveAsFlow()

    init {
        refreshNodes()
        _isConnected.value = Mobile.isRunning()
    }

    fun refreshNodes() {
        viewModelScope.launch {
            val saved = repository.loadNodes()
            _nodes.value = saved
            if (saved.isNotEmpty() && _currentNode.value.protocol == "none") {
                _currentNode.value = saved[0]
            }
        }
    }

    fun toggleConnection() {
        viewModelScope.launch {
            if (_isConnected.value) {
                _vpnEventChannel.send(VpnEvent.StopVpn)
                addLog("[系统] 正在请求停止服务...")
            } else {
                if (_currentNode.value.protocol != "none") {
                    val json = generateConfigJson(_currentNode.value)
                    _vpnEventChannel.send(VpnEvent.StartVpn(json))
                    addLog("[系统] 正在发起连接: ${_currentNode.value.tag}")
                } else {
                    addLog("[系统] 请先选择一个有效节点")
                }
            }
        }
    }

    fun selectNode(node: Node) {
        _currentNode.value = node
        addLog("[系统] 切换到节点: ${node.tag}")
    }

    // VPN 启动成功回调
    fun onVpnStarted() {
        _isConnected.value = true
        addLog("[核心] VPN 隧道已建立")
    }

    // VPN 停止回调
    fun onVpnStopped() {
        _isConnected.value = false
        addLog("[核心] VPN 隧道已关闭")
    }

    // 从文本导入节点
    fun importFromText(text: String, onResult: (Boolean, String) -> Unit) {
        val node = NodeParser.parse(text)
        if (node != null) {
            viewModelScope.launch {
                val currentList = _nodes.value.toMutableList()
                // 检查重复
                if (currentList.any { it.server == node.server && it.port == node.port }) {
                    onResult(false, "该节点已存在")
                    return@launch
                }
                currentList.add(node)
                repository.saveNodes(currentList)
                refreshNodes()
                addLog("[系统] 成功导入节点: ${node.tag}")
                onResult(true, "导入成功")
            }
        } else {
            addLog("[错误] 不支持的链接格式")
            onResult(false, "导入失败：链接格式无效")
        }
    }

    fun addLog(msg: String) {
        val current = _logs.value.toMutableList()
        if (current.size > 100) current.removeAt(0)
        current.add(msg)
        _logs.value = current
    }

    // 动态生成 Go 核心所需的 JSON 配置
    private fun generateConfigJson(node: Node): String {
        val useTls = node.protocol != "socks" && node.protocol != "shadowsocks"
        val sniValue = if (node.sni.isEmpty()) node.server else node.sni
        
        return """
        {
            "tag": "${node.tag}",
            "type": "${node.protocol}",
            "server": "${node.server}",
            "server_port": ${node.port},
            "password": "${node.password}",
            "uuid": "${node.uuid}",
            "tls": { 
                "enabled": $useTls, 
                "server_name": "$sniValue",
                "insecure": true 
            },
            "transport": { 
                "type": "${node.transport}", 
                "path": "${node.path}" 
            }
        }
        """.trimIndent()
    }
}
