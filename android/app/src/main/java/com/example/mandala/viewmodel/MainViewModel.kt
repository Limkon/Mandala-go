// 文件路徑: android/app/src/main/java/com/example/mandala/viewmodel/MainViewModel.kt

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
import java.io.File

// --- 數據模型定義 ---

data class AppStrings(
    val home: String, val profiles: String, val settings: String,
    val connect: String, val disconnect: String,
    val connected: String, val notConnected: String,
    val noNodeSelected: String,
    val nodeManagement: String, val importFromClipboard: String, val clipboardEmpty: String,
    val connectionSettings: String,
    val vpnMode: String, val vpnModeDesc: String,
    val allowInsecure: String, val allowInsecureDesc: String,
    val protocolSettings: String,
    val tlsFragment: String, val tlsFragmentDesc: String,
    val randomPadding: String, val randomPaddingDesc: String,
    val localPort: String,
    val appSettings: String, val theme: String, val language: String,
    val about: String, val confirm: String, val cancel: String,
    val edit: String, val delete: String, val save: String,
    val deleteConfirm: String,
    val tag: String, val address: String, val port: String,
    val password: String, val uuid: String, val sni: String
)

val ChineseStrings = AppStrings(
    home = "首頁", profiles = "節點", settings = "設置",
    connect = "連接", disconnect = "斷開",
    connected = "已連接", notConnected = "未連接",
    noNodeSelected = "請先選擇一個節點",
    nodeManagement = "節點管理", importFromClipboard = "從剪貼板導入", clipboardEmpty = "剪貼板為空",
    connectionSettings = "連接設置",
    vpnMode = "VPN 模式", vpnModeDesc = "通過 Mandala 路由所有設備流量",
    allowInsecure = "允許不安全連接", allowInsecureDesc = "跳過 TLS 證書驗證 (危險)",
    protocolSettings = "協議參數 (核心)",
    tlsFragment = "TLS 分片", tlsFragmentDesc = "拆分 TLS 記錄以繞過 DPI 檢測",
    randomPadding = "隨機填充", randomPaddingDesc = "向數據包添加隨機噪音",
    localPort = "本地監聽端口",
    appSettings = "應用設置", theme = "主題", language = "語言",
    about = "關於", confirm = "確定", cancel = "取消",
    edit = "編輯", delete = "刪除", save = "保存",
    deleteConfirm = "確定要刪除此節點嗎？",
    tag = "備註", address = "地址", port = "端口",
    password = "密碼", uuid = "UUID", sni = "SNI (域名)"
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

enum class AppThemeMode { SYSTEM, LIGHT, DARK }
enum class AppLanguage { CHINESE, ENGLISH }

// --- ViewModel 實現 ---

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val repository = NodeRepository(application)
    private val prefs = application.getSharedPreferences("mandala_settings", Context.MODE_PRIVATE)

    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _logs = MutableStateFlow(listOf("[系統] 準備就緒"))
    val logs = _logs.asStateFlow()

    private val _nodes = MutableStateFlow<List<Node>>(emptyList())
    val nodes = _nodes.asStateFlow()

    private val _currentNode = MutableStateFlow(Node("未選擇", "none", "0.0.0.0", 0))
    val currentNode = _currentNode.asStateFlow()

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

    // --- 設置更新 ---

    fun updateSetting(key: String, value: Boolean) {
        prefs.edit().putBoolean(key, value).apply()
        when (key) {
            "vpn_mode" -> _vpnMode.value = value
            "allow_insecure" -> _allowInsecure.value = value
            "tls_fragment" -> _tlsFragment.value = value
            "random_padding" -> _randomPadding.value = value
        }
    }

    fun updateLocalPort(port: String) {
        val p = port.toIntOrNull()
        if (p != null && p in 1024..65535) {
            prefs.edit().putInt("local_port", p).apply()
            _localPort.value = p
        }
    }

    fun updateTheme(mode: AppThemeMode) {
        prefs.edit().putInt("theme_mode", mode.ordinal).apply()
        _themeMode.value = mode
    }

    fun updateLanguage(lang: AppLanguage) {
        prefs.edit().putInt("app_language", lang.ordinal).apply()
        _language.value = lang
    }

    // --- 節點管理與連接 ---

    fun refreshNodes() {
        viewModelScope.launch {
            val saved = repository.loadNodes()
            _nodes.value = saved
            
            val lastSelected = saved.find { it.isSelected }
            
            if (lastSelected != null) {
                _currentNode.value = lastSelected
            } else if (saved.isNotEmpty() && _currentNode.value.protocol == "none") {
                _currentNode.value = saved[0]
            }
        }
    }

    fun toggleConnection() {
        viewModelScope.launch {
            if (_isConnected.value) {
                _vpnEventChannel.send(VpnEvent.StopVpn)
                addLog("[系統] 正在斷開...")
            } else {
                if (_currentNode.value.protocol != "none") {
                    val json = generateConfigJson(_currentNode.value)
                    _vpnEventChannel.send(VpnEvent.StartVpn(json))
                    addLog("[系統] 正在連接: ${_currentNode.value.tag}")
                } else {
                    addLog("[錯誤] ${appStrings.value.noNodeSelected}")
                }
            }
        }
    }

    fun selectNode(node: Node) {
        val updatedNode = node.copy(isSelected = true)
        _currentNode.value = updatedNode
        addLog("[系統] 已選擇: ${node.tag}")

        viewModelScope.launch {
            val currentList = _nodes.value.map { 
                if (it.server == node.server && it.port == node.port && 
                    it.protocol == node.protocol && it.tag == node.tag) {
                    it.copy(isSelected = true)
                } else {
                    it.copy(isSelected = false)
                }
            }
            _nodes.value = currentList
            repository.saveNodes(currentList)
        }
    }

    fun addNode(node: Node) {
        viewModelScope.launch {
            val currentList = _nodes.value.toMutableList()
            val nodeToSave = if (currentList.isEmpty()) node.copy(isSelected = true) else node.copy(isSelected = false)
            
            currentList.add(nodeToSave)
            repository.saveNodes(currentList)
            _nodes.value = currentList
            
            if (currentList.size == 1) {
                _currentNode.value = nodeToSave
            }
            
            addLog("[系統] 已添加: ${node.tag}")
        }
    }

    fun deleteNode(node: Node) {
        viewModelScope.launch {
            val currentList = _nodes.value.toMutableList()
            currentList.remove(node)
            repository.saveNodes(currentList)
            
            if (_currentNode.value == node) {
                 if (currentList.isNotEmpty()) {
                     val nextNode = currentList[0].copy(isSelected = true)
                     _currentNode.value = nextNode
                     val updatedList = currentList.mapIndexed { index, item ->
                         if (index == 0) item.copy(isSelected = true) else item.copy(isSelected = false)
                     }
                     _nodes.value = updatedList
                     repository.saveNodes(updatedList)
                 } else {
                     _currentNode.value = Node("未選擇", "none", "0.0.0.0", 0)
                     _nodes.value = emptyList()
                 }
            } else {
                _nodes.value = currentList
            }
            addLog("[系統] 已刪除: ${node.tag}")
        }
    }

    fun updateNode(oldNode: Node, newNode: Node) {
        viewModelScope.launch {
            val currentList = _nodes.value.toMutableList()
            val index = currentList.indexOf(oldNode)
            if (index != -1) {
                val isSelected = oldNode.isSelected || (_currentNode.value == oldNode)
                val nodeToSave = newNode.copy(isSelected = isSelected)
                
                currentList[index] = nodeToSave
                repository.saveNodes(currentList)
                
                if (_currentNode.value == oldNode) {
                    _currentNode.value = nodeToSave
                }
                
                _nodes.value = currentList
                addLog("[系統] 已更新: ${newNode.tag}")
            }
        }
    }

    fun onVpnStarted() {
        _isConnected.value = true
        addLog("[核心] 已連通網絡")
    }

    fun onVpnStopped() {
        _isConnected.value = false
        addLog("[核心] 連接已關閉")
    }

    fun importFromText(text: String, onResult: (Boolean, String) -> Unit) {
        val newNodes = NodeParser.parseList(text)
        
        if (newNodes.isNotEmpty()) {
            viewModelScope.launch {
                val current = _nodes.value.toMutableList()
                var addedCount = 0
                
                for (node in newNodes) {
                    val exists = current.any { 
                        it.server == node.server && 
                        it.port == node.port && 
                        it.protocol == node.protocol 
                    }
                    
                    if (!exists) {
                        current.add(node.copy(isSelected = false))
                        addedCount++
                    }
                }

                if (addedCount > 0) {
                    repository.saveNodes(current)
                    refreshNodes()
                    val msg = "成功導入 $addedCount 個節點" + if (newNodes.size > addedCount) " (過濾 ${newNodes.size - addedCount} 個重複)" else ""
                    addLog("[系統] $msg")
                    onResult(true, msg)
                } else {
                    onResult(false, "節點已存在，未導入新節點")
                }
            }
        } else {
            onResult(false, "未識別到有效的節點鏈接")
        }
    }

    fun addLog(msg: String) {
        val current = _logs.value.toMutableList()
        if (current.size > 100) current.removeAt(0)
        current.add(msg)
        _logs.value = current
    }

    private fun generateConfigJson(node: Node): String {
        // [關鍵修復] 移除了對 Shadowsocks 和 Socks5 的 TLS 硬屏蔽
        // 只要節點配置了傳輸方式（如 ws）或 SNI，就應該允許啟用 TLS 以符合大多數服務端配置
        val useTls = node.sni.isNotEmpty() || node.transport == "ws" || node.port == 443

        val logDir = getApplication<Application>().getExternalFilesDir(null)
        val logFile = if (logDir != null) {
            File(logDir, "mandala_core.log").absolutePath
        } else {
            getApplication<Application>().filesDir.absolutePath + "/mandala_core.log"
        }

        return """
        {
            "tag": "${node.tag}",
            "type": "${node.protocol}",
            "server": "${node.server}",
            "server_port": ${node.port},
            "password": "${node.password}",
            "uuid": "${node.uuid}",
            "username": "${if(node.protocol == "socks5") node.uuid else ""}",
            "log_path": "$logFile",
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
