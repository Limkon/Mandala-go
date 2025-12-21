package com.example.mandala.viewmodel

import android.app.Application
import android.content.Context
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.example.mandala.data.NodeRepository
import com.example.mandala.utils.NodeParser
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
    val path: String = "/",
    val sni: String = "",
    val isSelected: Boolean = false
)

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val repository = NodeRepository(application)
    // [新增] 用于保存设置
    private val prefs = application.getSharedPreferences("mandala_settings", Context.MODE_PRIVATE)

    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _logs = MutableStateFlow(listOf("[系统] 准备就绪"))
    val logs = _logs.asStateFlow()

    private val _nodes = MutableStateFlow<List<Node>>(emptyList())
    val nodes = _nodes.asStateFlow()

    private val _currentNode = MutableStateFlow(Node("未选择", "none", "0.0.0.0", 0))
    val currentNode = _currentNode.asStateFlow()

    // [新增] 设置状态流，初始值从 Prefs 读取
    private val _vpnMode = MutableStateFlow(prefs.getBoolean("vpn_mode", true))
    val vpnMode = _vpnMode.asStateFlow()

    private val _allowInsecure = MutableStateFlow(prefs.getBoolean("allow_insecure", false))
    val allowInsecure = _allowInsecure.asStateFlow()

    private val _tlsFragment = MutableStateFlow(prefs.getBoolean("tls_fragment", true))
    val tlsFragment = _tlsFragment.asStateFlow()

    private val _randomPadding = MutableStateFlow(prefs.getBoolean("random_padding", false))
    val randomPadding = _randomPadding.asStateFlow()

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

    // [新增] 更新并保存设置
    fun updateSetting(key: String, value: Boolean) {
        prefs.edit().putBoolean(key, value).apply()
        when (key) {
            "vpn_mode" -> _vpnMode.value = value
            "allow_insecure" -> _allowInsecure.value = value
            "tls_fragment" -> _tlsFragment.value = value
            "random_padding" -> _randomPadding.value = value
        }
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
                addLog("[系统] 正在断开连接...")
            } else {
                if (_currentNode.value.protocol != "none") {
                    val json = generateConfigJson(_currentNode.value)
                    _vpnEventChannel.send(VpnEvent.StartVpn(json))
                    addLog("[系统] 正在连接: ${_currentNode.value.tag}")
                } else {
                    addLog("[错误] 请先选择一个节点")
                }
            }
        }
    }

    fun selectNode(node: Node) {
        _currentNode.value = node
        addLog("[系统] 已选择: ${node.tag}")
    }

    // 核心接口修复
    fun onVpnStarted() {
        _isConnected.value = true
        addLog("[核心] 已连通网络")
    }

    fun onVpnStopped() {
        _isConnected.value = false
        addLog("[核心] 连接已关闭")
    }

    // 导入接口修复
    fun importFromText(text: String, onResult: (Boolean, String) -> Unit) {
        val node = NodeParser.parse(text)
        if (node != null) {
            viewModelScope.launch {
                val current = _nodes.value.toMutableList()
                if (current.any { it.server == node.server && it.port == node.port }) {
                    onResult(false, "节点已存在")
                    return@launch
                }
                current.add(node)
                repository.saveNodes(current)
                refreshNodes()
                addLog("[系统] 导入成功: ${node.tag}")
                onResult(true, "导入成功")
            }
        } else {
            onResult(false, "无效的链接格式")
        }
    }

    fun addLog(msg: String) {
        val current = _logs.value.toMutableList()
        if (current.size > 100) current.removeAt(0)
        current.add(msg)
        _logs.value = current
    }

    // [修改] 生成 Config JSON 时加入设置参数
    private fun generateConfigJson(node: Node): String {
        val useTls = node.protocol != "socks" && node.protocol != "shadowsocks"
        val insecure = _allowInsecure.value
        // 目前 Go 核心结构体可能未完全支持 fragment/padding，但我们将数据传过去以便未来兼容或扩展
        val fragment = _tlsFragment.value
        val padding = _randomPadding.value

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
                "server_name": "${if (node.sni.isEmpty()) node.server else node.sni}",
                "insecure": $insecure
            },
            "transport": { "type": "${node.transport}", "path": "${node.path}" },
            "settings": {
                "vpn_mode": ${_vpnMode.value},
                "fragment": $fragment,
                "noise": $padding
            }
        }
        """.trimIndent()
    }
}
