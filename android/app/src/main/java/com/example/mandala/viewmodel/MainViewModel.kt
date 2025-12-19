package com.example.mandala.viewmodel

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.example.mandala.data.NodeRepository
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import mobile.Mobile

data class Node(
    val tag: String,
    val protocol: String,
    val server: String,
    val port: Int,
    val password: String = "",
    val uuid: String = "",
    val transport: String = "tcp",
    val path: String = "/",    // 增加路徑支持
    val sni: String = "",      // 增加 SNI 支持
    val isSelected: Boolean = false
)

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val repository = NodeRepository(application)

    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _logs = MutableStateFlow(listOf("[系統] 準備就緒"))
    val logs = _logs.asStateFlow()

    private val _nodes = MutableStateFlow<List<Node>>(emptyList())
    val nodes = _nodes.asStateFlow()

    private val _currentNode = MutableStateFlow(Node("未選擇", "none", "0.0.0.0", 0))
    val currentNode = _currentNode.asStateFlow()

    sealed class VpnEvent {
        data class StartVpn(val configJson: String) : VpnEvent()
        object StopVpn : VpnEvent()
    }
    private val _vpnEventChannel = Channel<VpnEvent>()
    val vpnEvent = _vpnEventChannel.receiveAsFlow()

    init {
        viewModelScope.launch {
            val saved = repository.loadNodes()
            _nodes.value = saved
            if (saved.isNotEmpty()) _currentNode.value = saved[0]
            _isConnected.value = Mobile.isRunning()
        }
    }

    fun toggleConnection() {
        viewModelScope.launch {
            if (_isConnected.value) {
                _vpnEventChannel.send(VpnEvent.StopVpn)
                _isConnected.value = false
                addLog("[系統] 正在停止服務...")
            } else {
                if (_currentNode.value.protocol != "none") {
                    val json = generateConfigJson(_currentNode.value)
                    _vpnEventChannel.send(VpnEvent.StartVpn(json))
                    _isConnected.value = true
                    addLog("[系統] 正在發起連線: ${_currentNode.value.tag}")
                }
            }
        }
    }

    fun selectNode(node: Node) {
        _currentNode.value = node
        addLog("[系統] 已選擇節點: ${node.tag}")
    }

    fun onVpnStopped() {
        _isConnected.value = false
        addLog("[核心] 服務已斷開")
    }

    private fun addLog(msg: String) {
        val current = _logs.value.toMutableList()
        if (current.size > 50) current.removeAt(0)
        current.add(msg)
        _logs.value = current
    }

    // [核心修復] 動態生成配置 JSON，包含 Path 和 SNI
    private fun generateConfigJson(node: Node): String {
        val useTls = node.protocol != "socks" && node.protocol != "shadowsocks"
        // 如果節點沒有指定 SNI，則默認使用伺服器地址
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
