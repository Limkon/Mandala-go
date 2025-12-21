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

// [修改] 多语言字符串封装，增加编辑/删除相关字段
data class AppStrings(
    val home: String,
    val profiles: String,
    val settings: String,
    val connect: String,
    val disconnect: String,
    val connected: String,
    val notConnected: String,
    val noNodeSelected: String,
    val nodeManagement: String,
    val importFromClipboard: String,
    val clipboardEmpty: String,
    val connectionSettings: String,
    val vpnMode: String,
    val vpnModeDesc: String,
    val allowInsecure: String,
    val allowInsecureDesc: String,
    val protocolSettings: String,
    val tlsFragment: String,
    val tlsFragmentDesc: String,
    val randomPadding: String,
    val randomPaddingDesc: String,
    val localPort: String,
    val appSettings: String,
    val theme: String,
    val language: String,
    val about: String,
    val confirm: String,
    val cancel: String,
    // 新增字段
    val edit: String,
    val delete: String,
    val save: String,
    val deleteConfirm: String,
    val tag: String,
    val address: String,
    val port: String,
    val password: String,
    val uuid: String,
    val sni: String
)

val ChineseStrings = AppStrings(
    home = "首页", profiles = "节点", settings = "设置",
    connect = "连接", disconnect = "断开",
    connected = "已连接", notConnected = "未连接",
    noNodeSelected = "请先选择一个节点",
    nodeManagement = "节点管理", importFromClipboard = "从剪贴板导入", clipboardEmpty = "剪贴板为空",
    connectionSettings = "连接设置",
    vpnMode = "VPN 模式", vpnModeDesc = "通过 Mandala 路由所有设备流量",
    allowInsecure = "允许不安全连接", allowInsecureDesc = "跳过 TLS 证书验证 (危险)",
    protocolSettings = "协议参数 (核心)",
    tlsFragment = "TLS 分片", tlsFragmentDesc = "拆分 TLS 记录以绕过 DPI 检测",
    randomPadding = "随机填充", randomPaddingDesc = "向数据包添加随机噪音",
    localPort = "本地监听端口",
    appSettings = "应用设置", theme = "主题", language = "语言",
    about = "关于", confirm = "确定", cancel = "取消",
    // 新增
    edit = "编辑", delete = "删除", save = "保存",
    deleteConfirm = "确定要删除此节点吗？",
    tag = "备注", address = "地址", port = "端口",
    password = "密码", uuid = "UUID", sni = "SNI (域名)"
)

val EnglishStrings = AppStrings(
    home = "Home", profiles = "Profiles", settings = "Settings",
    connect = "Connect", disconnect = "Disconnect",
    connected = "Connected", notConnected = "Disconnected",
    noNodeSelected = "Please select a node first",
    nodeManagement = "Profiles", importFromClipboard = "Import from Clipboard", clipboardEmpty = "Clipboard is empty",
    connectionSettings = "Connection",
    vpnMode = "VPN Mode", vpnModeDesc = "Route all traffic through Mandala",
    allowInsecure = "Insecure", allowInsecureDesc = "Skip TLS verification (Dangerous)",
    protocolSettings = "Protocol",
    tlsFragment = "TLS Fragment", tlsFragmentDesc = "Split TLS records to bypass DPI",
    randomPadding = "Random Padding", randomPaddingDesc = "Add random noise to packets",
    localPort = "Local Port",
    appSettings = "App Settings", theme = "Theme", language = "Language",
    about = "About", confirm = "OK", cancel = "Cancel",
    // 新增
    edit = "Edit", delete = "Delete", save = "Save",
    deleteConfirm = "Are you sure you want to delete this node?",
    tag = "Tag", address = "Address", port = "Port",
    password = "Password", uuid = "UUID", sni = "SNI"
)

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

// 主题枚举
enum class AppThemeMode { SYSTEM, LIGHT, DARK }
// 语言枚举
enum class AppLanguage { CHINESE, ENGLISH }

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val repository = NodeRepository(application)
    private val prefs = application.getSharedPreferences("mandala_settings", Context.MODE_PRIVATE)

    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _logs = MutableStateFlow(listOf("[系统] 准备就绪"))
    val logs = _logs.asStateFlow()

    private val _nodes = MutableStateFlow<List<Node>>(emptyList())
    val nodes = _nodes.asStateFlow()

    private val _currentNode = MutableStateFlow(Node("未选择", "none", "0.0.0.0", 0))
    val currentNode = _currentNode.asStateFlow()

    // 设置状态
    private val _vpnMode = MutableStateFlow(prefs.getBoolean("vpn_mode", true))
    val vpnMode = _vpnMode.asStateFlow()

    private val _allowInsecure = MutableStateFlow(prefs.getBoolean("allow_insecure", false))
    val allowInsecure = _allowInsecure.asStateFlow()

    private val _tlsFragment = MutableStateFlow(prefs.getBoolean("tls_fragment", true))
    val tlsFragment = _tlsFragment.asStateFlow()

    private val _randomPadding = MutableStateFlow(prefs.getBoolean("random_padding", false))
    val randomPadding = _randomPadding.asStateFlow()

    private val _localPort = MutableStateFlow(prefs.getInt("local_port", 10809))
    val localPort = _localPort.asStateFlow()

    private val _themeMode = MutableStateFlow(
        AppThemeMode.values()[prefs.getInt("theme_mode", AppThemeMode.SYSTEM.ordinal)]
    )
    val themeMode = _themeMode.asStateFlow()

    private val _language = MutableStateFlow(
        AppLanguage.values()[prefs.getInt("app_language", AppLanguage.CHINESE.ordinal)]
    )
    val language = _language.asStateFlow()

    val appStrings = _language.map { 
        if (it == AppLanguage.ENGLISH) EnglishStrings else ChineseStrings 
    }.stateIn(viewModelScope, SharingStarted.Eagerly, ChineseStrings)

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

    // 更新 Boolean 设置
    fun updateSetting(key: String, value: Boolean) {
        prefs.edit().putBoolean(key, value).apply()
        when (key) {
            "vpn_mode" -> _vpnMode.value = value
            "allow_insecure" -> _allowInsecure.value = value
            "tls_fragment" -> _tlsFragment.value = value
            "random_padding" -> _randomPadding.value = value
        }
    }

    // 更新端口设置
    fun updateLocalPort(port: String) {
        val p = port.toIntOrNull()
        if (p != null && p in 1024..65535) {
            prefs.edit().putInt("local_port", p).apply()
            _localPort.value = p
        }
    }

    // 更新主题
    fun updateTheme(mode: AppThemeMode) {
        prefs.edit().putInt("theme_mode", mode.ordinal).apply()
        _themeMode.value = mode
    }

    // 更新语言
    fun updateLanguage(lang: AppLanguage) {
        prefs.edit().putInt("app_language", lang.ordinal).apply()
        _language.value = lang
    }

    fun refreshNodes() {
        viewModelScope.launch {
            val saved = repository.loadNodes()
            _nodes.value = saved
            // 如果列表不为空且当前未选择节点，默认选择第一个
            if (saved.isNotEmpty() && _currentNode.value.protocol == "none") {
                _currentNode.value = saved[0]
            }
        }
    }

    fun toggleConnection() {
        viewModelScope.launch {
            if (_isConnected.value) {
                _vpnEventChannel.send(VpnEvent.StopVpn)
                addLog("[系统] 正在断开...")
            } else {
                if (_currentNode.value.protocol != "none") {
                    val json = generateConfigJson(_currentNode.value)
                    _vpnEventChannel.send(VpnEvent.StartVpn(json))
                    addLog("[系统] 正在连接: ${_currentNode.value.tag}")
                } else {
                    addLog("[错误] ${appStrings.value.noNodeSelected}")
                }
            }
        }
    }

    fun selectNode(node: Node) {
        _currentNode.value = node
        addLog("[系统] 已选择: ${node.tag}")
    }

    // [新增] 删除节点
    fun deleteNode(node: Node) {
        viewModelScope.launch {
            val currentList = _nodes.value.toMutableList()
            // 简单对象比较，如果有唯一 ID 更好，但这里用数据类相等性即可
            currentList.remove(node)
            repository.saveNodes(currentList)
            
            // 如果删除的是当前选中的节点，重置选中状态
            if (_currentNode.value == node) {
                 if (currentList.isNotEmpty()) {
                     _currentNode.value = currentList[0]
                 } else {
                     _currentNode.value = Node("未选择", "none", "0.0.0.0", 0)
                 }
                 // 如果正在连接，可能需要断开，这里暂不强制断开，仅更新 UI 状态
            }
            
            _nodes.value = currentList
            addLog("[系统] 已删除: ${node.tag}")
        }
    }

    // [新增] 更新节点
    fun updateNode(oldNode: Node, newNode: Node) {
        viewModelScope.launch {
            val currentList = _nodes.value.toMutableList()
            val index = currentList.indexOf(oldNode)
            if (index != -1) {
                currentList[index] = newNode
                repository.saveNodes(currentList)
                
                // 如果更新的是当前选中的节点，需要同步更新选中状态
                if (_currentNode.value == oldNode) {
                    _currentNode.value = newNode
                    // 如果正在运行，配置不会实时生效，需要重连
                }
                
                _nodes.value = currentList
                addLog("[系统] 已更新: ${newNode.tag}")
            }
        }
    }

    fun onVpnStarted() {
        _isConnected.value = true
        addLog("[核心] 已连通网络")
    }

    fun onVpnStopped() {
        _isConnected.value = false
        addLog("[核心] 连接已关闭")
    }

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

    private fun generateConfigJson(node: Node): String {
        val useTls = node.protocol != "socks" && node.protocol != "shadowsocks"
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
                "insecure": ${_allowInsecure.value}
            },
            "transport": { "type": "${node.transport}", "path": "${node.path}" },
            "settings": {
                "vpn_mode": ${_vpnMode.value},
                "fragment": ${_tlsFragment.value},
                "noise": ${_randomPadding.value}
            },
            "local_port": ${_localPort.value}
        }
        """.trimIndent()
    }
}
