// 文件路径: android/app/src/main/java/com/example/mandala/ui/profiles/ProfilesScreen.kt

package com.example.mandala.ui.profiles

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import com.example.mandala.viewmodel.MainViewModel
import com.example.mandala.viewmodel.Node

@Composable
fun ProfilesScreen(viewModel: MainViewModel) {
    val nodes by viewModel.nodes.collectAsState()
    var showEditDialog by remember { mutableStateOf(false) }
    var currentNode by remember { mutableStateOf<Node?>(null) }

    Scaffold(
        floatingActionButton = {
            FloatingActionButton(onClick = {
                currentNode = null // 新建模式
                showEditDialog = true
            }) {
                Icon(Icons.Default.Add, contentDescription = "Add Node")
            }
        }
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
        ) {
            items(nodes) { node ->
                NodeItem(
                    node = node,
                    onEdit = {
                        currentNode = node
                        showEditDialog = true
                    },
                    onDelete = { viewModel.deleteNode(node) },
                    onSelect = { viewModel.selectNode(node) }
                )
            }
        }
    }

    if (showEditDialog) {
        NodeEditDialog(
            node = currentNode,
            onDismiss = { showEditDialog = false },
            onSave = { newNode ->
                if (currentNode == null) {
                    viewModel.addNode(newNode)
                } else {
                    viewModel.updateNode(currentNode!!, newNode)
                }
                showEditDialog = false
            }
        )
    }
}

@Composable
fun NodeItem(
    node: Node,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    onSelect: () -> Unit
) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(8.dp)
            .clickable { onSelect() },
        elevation = CardDefaults.cardElevation(4.dp)
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(text = node.tag, style = MaterialTheme.typography.titleMedium)
                Text(
                    text = "${node.protocol}://${node.server}:${node.port}",
                    style = MaterialTheme.typography.bodySmall
                )
                // 显示额外信息提示，方便确认配置
                if (node.transport == "ws") {
                    Text(text = "WS | ${node.path}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
                }
            }
            IconButton(onClick = onEdit) {
                Icon(Icons.Default.Edit, contentDescription = "Edit")
            }
            IconButton(onClick = onDelete) {
                Icon(Icons.Default.Delete, contentDescription = "Delete")
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeEditDialog(
    node: Node?,
    onDismiss: () -> Unit,
    onSave: (Node) -> Unit
) {
    // 初始化状态，如果 node 为 null 则为空白默认值
    var tag by remember { mutableStateOf(node?.tag ?: "新节点") }
    var protocol by remember { mutableStateOf(node?.protocol ?: "vless") }
    var server by remember { mutableStateOf(node?.server ?: "") }
    var port by remember { mutableStateOf(node?.port?.toString() ?: "443") }
    var password by remember { mutableStateOf(node?.password ?: "") }
    var uuid by remember { mutableStateOf(node?.uuid ?: "") }
    
    // [新增] 高级传输字段
    var transport by remember { mutableStateOf(node?.transport ?: "tcp") }
    var sni by remember { mutableStateOf(node?.sni ?: "") }
    var path by remember { mutableStateOf(node?.path ?: "/") }
    
    var showAdvanced by remember { mutableStateOf(false) }

    // 协议列表
    val protocols = listOf("vless", "vmess", "trojan", "shadowsocks", "socks5", "mandala")
    val transports = listOf("tcp", "ws")

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(text = if (node == null) "添加节点" else "编辑节点") },
        text = {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
            ) {
                // 1. 基础信息
                OutlinedTextField(
                    value = tag, onValueChange = { tag = it },
                    label = { Text("备注 (Tag)") }, modifier = Modifier.fillMaxWidth()
                )
                
                // 协议选择 (简单的 Dropdown 模拟)
                // 实际项目中可以使用 ExposedDropdownMenuBox，这里简化为点击切换或输入
                Spacer(modifier = Modifier.height(8.dp))
                Text("协议类型:", style = MaterialTheme.typography.labelMedium)
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    protocols.forEach { p ->
                        FilterChip(
                            selected = (protocol == p),
                            onClick = { protocol = p },
                            label = { Text(p) }
                        )
                    }
                }

                Spacer(modifier = Modifier.height(8.dp))
                Row(modifier = Modifier.fillMaxWidth()) {
                    OutlinedTextField(
                        value = server, onValueChange = { server = it },
                        label = { Text("地址 (Host)") }, modifier = Modifier.weight(0.7f)
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    OutlinedTextField(
                        value = port, onValueChange = { port = it },
                        label = { Text("端口") }, modifier = Modifier.weight(0.3f)
                    )
                }

                // 2. 认证信息 (根据协议动态变化标签)
                Spacer(modifier = Modifier.height(8.dp))
                val uuidLabel = when (protocol) {
                    "shadowsocks" -> "加密方式 (Cipher)"
                    "socks5" -> "用户名 (User)"
                    else -> "UUID / 用户ID"
                }
                
                OutlinedTextField(
                    value = uuid, onValueChange = { uuid = it },
                    label = { Text(uuidLabel) }, modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(8.dp))
                OutlinedTextField(
                    value = password, onValueChange = { password = it },
                    label = { Text("密码 (Password)") },
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth()
                )

                // 3. 高级/传输设置
                Spacer(modifier = Modifier.height(16.dp))
                TextButton(onClick = { showAdvanced = !showAdvanced }) {
                    Text(if (showAdvanced) "收起高级设置" else "展开高级设置 (传输/TLS)")
                }

                if (showAdvanced) {
                    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)) {
                        Column(modifier = Modifier.padding(8.dp)) {
                            // 传输协议选择
                            Text("传输方式 (Transport):", style = MaterialTheme.typography.labelMedium)
                            Row {
                                transports.forEach { t ->
                                    FilterChip(
                                        selected = (transport == t),
                                        onClick = { transport = t },
                                        label = { Text(t.uppercase()) },
                                        modifier = Modifier.padding(end = 8.dp)
                                    )
                                }
                            }

                            // WS 路径
                            if (transport == "ws") {
                                OutlinedTextField(
                                    value = path, onValueChange = { path = it },
                                    label = { Text("WebSocket 路径 (Path)") },
                                    placeholder = { Text("/ws") },
                                    modifier = Modifier.fillMaxWidth()
                                )
                            }

                            Spacer(modifier = Modifier.height(8.dp))
                            // SNI
                            OutlinedTextField(
                                value = sni, onValueChange = { sni = it },
                                label = { Text("伪装域名 (SNI)") },
                                placeholder = { Text("留空则使用Host") },
                                modifier = Modifier.fillMaxWidth()
                            )
                        }
                    }
                }
            }
        },
        confirmButton = {
            Button(onClick = {
                val p = port.toIntOrNull() ?: 443
                val newNode = Node(
                    tag = tag,
                    protocol = protocol,
                    server = server,
                    port = p,
                    password = password,
                    uuid = uuid,
                    transport = transport,
                    path = path,
                    sni = sni
                )
                onSave(newNode)
            }) {
                Text("保存")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        }
    )
}
