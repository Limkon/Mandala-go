// 文件路径: android/app/src/main/java/com/example/mandala/viewmodel/MainViewModel.kt

package com.example.mandala.viewmodel

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.example.mandala.data.NodeRepository
import com.example.mandala.utils.NodeParser
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import mobile.Mobile

// 保持 Node 数据结构定义不变
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

// 修改: 继承 AndroidViewModel 以获取 Application Context
class MainViewModel(application: Application) : AndroidViewModel(application) {
    
    private val repository = NodeRepository(application)

    // --- UI 状态流 ---
    private val _isConnected = MutableStateFlow(false)
    val isConnected = _isConnected.asStateFlow()

    private val _connectionTime = MutableStateFlow("00:00:00")
    val connectionTime = _connectionTime.asStateFlow()

    // 当前选中的节点 (默认给一个空状态，等加载完成后更新)
    private val _currentNode = MutableStateFlow(
        Node("未选择节点", "none", "0.0.0.0", 0)
    )
    val currentNode = _currentNode.asStateFlow()

    private val _logs = MutableStateFlow(listOf("[系统] 就绪"))
    val logs = _logs.asStateFlow()

    // 节点列表: 初始为空，移除硬编码示例
    private val _nodes = MutableStateFlow<List<Node>>(emptyList())
    val nodes = _nodes.asStateFlow()

    // --- 初始化 ---
    init {
        loadData()
    }

    private fun loadData() {
        viewModelScope.launch {
            val savedNodes = repository.loadNodes()
            _nodes.value = savedNodes
            
            // 如果有节点，默认选中第一个
            if (savedNodes.isNotEmpty()) {
                _currentNode.value = savedNodes[0]
                addLog("[系统] 已加载 ${savedNodes.size} 个节点")
            } else {
                addLog("[系统] 暂无节点，请添加")
            }
        }
    }

    // --- 核心操作 ---

    fun toggleConnection() {
        if (_isConnected.value) {
            stopProxy()
        } else {
            // 简单校验
            if (_currentNode.value.protocol == "none") {
                addLog("[错误] 请先选择有效节点")
                return
            }
            startProxy()
        }
    }

    private fun startProxy() {
        viewModelScope.launch {
            try {
                addLog("[核心] 正在准备配置...")
                val configJson = generateConfigJson(_currentNode.value)
                
                addLog("[核心] 正在启动服务 (端口 10809)...")
                val error = Mobile.start(10809, configJson)

                if (error.isEmpty()) {
                    _isConnected.value = true
                    addLog("[核心] 服务启动成功")
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
        if (_isConnected.value) {
            stopProxy()
        }
        _currentNode.value = node
        addLog("[系统] 切换到节点: ${node.tag}")
    }

    // 添加节点 (带持久化)
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
        
        updateListAndSave(currentList)
    }

    // [新增] 删除节点 (带持久化) - 既然有了保存，通常也需要删除
    fun deleteNode(node: Node) {
        val currentList = _nodes.value.toMutableList()
        val removed = currentList.removeIf { it.tag == node.tag && it.server == node.server }
        
        if (removed) {
            addLog("[系统] 删除节点: ${node.tag}")
            updateListAndSave(currentList)
            
            // 如果删除的是当前选中节点，重置状态
            if (_currentNode.value.tag == node.tag && _currentNode.value.server == node.server) {
                if (currentList.isNotEmpty()) {
                    _currentNode.value = currentList[0]
                } else {
                    _currentNode.value = Node("未选择节点", "none", "0.0.0.0", 0)
                }
            }
        }
    }

    // 导入节点
    fun importFromText(text: String): Boolean {
        val node = NodeParser.parse(text)
        return if (node != null) {
            addNode(node)
            true
        } else {
            addLog("[错误] 无法解析内容")
            false
        }
    }

    private fun updateListAndSave(newList: List<Node>) {
        _nodes.value = newList
        viewModelScope.launch {
            repository.saveNodes(newList)
        }
    }

    private fun addLog(msg: String) {
        val currentLogs = _logs.value.toMutableList()
        if (currentLogs.size > 100) currentLogs.removeAt(0)
        currentLogs.add(msg)
        _logs.value = currentLogs
    }

    private fun generateConfigJson(node: Node): String {
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
