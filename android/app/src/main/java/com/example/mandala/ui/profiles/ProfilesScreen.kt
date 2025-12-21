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
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.example.mandala.viewmodel.MainViewModel
import com.example.mandala.viewmodel.Node
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProfilesScreen(viewModel: MainViewModel) {
    val nodes by viewModel.nodes.collectAsState()
    var showEditDialog by remember { mutableStateOf(false) }
    var currentNode by remember { mutableStateOf<Node?>(null) }
    
    // 剪贴板管理器
    val clipboardManager = LocalClipboardManager.current
    val snackbarHostState = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        floatingActionButton = {
            Column(
                horizontalAlignment = Alignment.End,
                verticalArrangement = Arrangement.spacedBy(16.dp)
            ) {
                // [修复] 找回剪贴板导入功能
                SmallFloatingActionButton(
                    onClick = {
                        val clipData = clipboardManager.getText()
                        if (!clipData.isNullOrBlank()) {
                            viewModel.importFromText(clipData.text) { success, msg ->
                                scope.launch {
                                    snackbarHostState.showSnackbar(msg)
                                }
                            }
                        } else {
                            scope.launch {
                                snackbarHostState.showSnackbar("剪贴板为空")
                            }
                        }
                    },
                    containerColor = MaterialTheme.colorScheme.secondaryContainer,
                    contentColor = MaterialTheme.colorScheme.onSecondaryContainer
                ) {
                    Icon(Icons.Default.ContentPaste, contentDescription = "Import from Clipboard")
                }

                // 添加节点按钮
                FloatingActionButton(onClick = {
                    currentNode = null // 新建模式
                    showEditDialog = true
                }) {
                    Icon(Icons.Default.Add, contentDescription = "Add Node")
                }
            }
        }
    ) { padding ->
        if (nodes.isEmpty()) {
            Box(modifier = Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                Text("暂无节点，请点击右下角导入或添加", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } else {
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
    val cardColors = if (node.isSelected) {
        CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)
    } else {
        CardDefaults.cardColors()
    }

    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .clickable { onSelect() },
        elevation = CardDefaults.cardElevation(2.dp),
        colors = cardColors
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(text = node.tag, style = MaterialTheme.typography.titleMedium)
                    if (node.isSelected) {
                        Spacer(modifier = Modifier.width(8.dp))
                        Icon(Icons.Default.CheckCircle, contentDescription = "Selected", modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary)
                    }
                }
                Spacer(modifier = Modifier.height(4.dp))
                Text(
                    text = "${node.protocol.uppercase()} | ${node.server}:${node.port}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                if (node.transport == "ws") {
                    Text(text = "WebSocket: ${node.path}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.tertiary)
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
    var tag by remember { mutableStateOf(node?.tag ?: "新节点") }
    // [修复] 默认协议改为 vless，避免 vmess 出现在默认值
    var protocol by remember { mutableStateOf(node?.protocol ?: "vless") }
    var server by remember { mutableStateOf(node?.server ?: "") }
    var port by remember { mutableStateOf(node?.port?.toString() ?: "443") }
    var password by remember { mutableStateOf(node?.password ?: "") }
    var uuid by remember { mutableStateOf(node?.uuid ?: "") }
    
    var transport by remember { mutableStateOf(node?.transport ?: "tcp") }
    var sni by remember { mutableStateOf(node?.sni ?: "") }
    var path by remember { mutableStateOf(node?.path ?: "/") }
    
    var showAdvanced by remember { mutableStateOf(false) }
    var expandedProtocol by remember { mutableStateOf(false) }

    // [修复] 移除 vmess，防止手动添加
    val protocols = listOf("vless", "trojan", "shadowsocks", "socks5", "mandala")
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
                    label = { Text("备注 (Tag)") }, 
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true
                )
                
                Spacer(modifier = Modifier.height(8.dp))

                // [修复] 布局混乱：改为下拉菜单选择协议
                ExposedDropdownMenuBox(
                    expanded = expandedProtocol,
                    onExpandedChange = { expandedProtocol = !expandedProtocol },
                    modifier = Modifier.fillMaxWidth()
                ) {
                    OutlinedTextField(
                        value = protocol.uppercase(),
                        onValueChange = {},
                        readOnly = true,
                        label = { Text("协议类型") },
                        trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expandedProtocol) },
                        colors = ExposedDropdownMenuDefaults.outlinedTextFieldColors(),
                        modifier = Modifier.menuAnchor().fillMaxWidth()
                    )
                    ExposedDropdownMenu(
                        expanded = expandedProtocol,
                        onDismissRequest = { expandedProtocol = false }
                    ) {
                        protocols.forEach { p ->
                            DropdownMenuItem(
                                text = { Text(p.uppercase()) },
                                onClick = {
                                    protocol = p
                                    expandedProtocol = false
                                }
                            )
                        }
                    }
                }

                Spacer(modifier = Modifier.height(8.dp))
                
                // 地址和端口放在一行
                Row(modifier = Modifier.fillMaxWidth()) {
                    OutlinedTextField(
                        value = server, onValueChange = { server = it },
                        label = { Text("地址 (Host)") }, 
                        modifier = Modifier.weight(0.65f),
                        singleLine = true
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    OutlinedTextField(
                        value = port, onValueChange = { port = it },
                        label = { Text("端口") }, 
                        modifier = Modifier.weight(0.35f),
                        singleLine = true
                    )
                }

                // 2. 认证信息
                Spacer(modifier = Modifier.height(8.dp))
                val uuidLabel = when (protocol) {
                    "shadowsocks" -> "加密方式 (Cipher)"
                    "socks5" -> "用户名 (User)"
                    else -> "UUID / 用户ID"
                }
                
                OutlinedTextField(
                    value = uuid, onValueChange = { uuid = it },
                    label = { Text(uuidLabel) }, 
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true
                )

                Spacer(modifier = Modifier.height(8.dp))
                OutlinedTextField(
                    value = password, onValueChange = { password = it },
                    label = { Text("密码 (Password)") },
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true
                )

                // 3. 高级设置
                Spacer(modifier = Modifier.height(16.dp))
                TextButton(
                    onClick = { showAdvanced = !showAdvanced },
                    modifier = Modifier.align(Alignment.Start)
                ) {
                    Text(if (showAdvanced) "收起高级设置" else "展开高级设置 (WS/TLS)")
                }

                if (showAdvanced) {
                    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)) {
                        Column(modifier = Modifier.padding(12.dp)) {
                            Text("传输方式", style = MaterialTheme.typography.labelMedium)
                            Row(modifier = Modifier.fillMaxWidth()) {
                                transports.forEach { t ->
                                    FilterChip(
                                        selected = (transport == t),
                                        onClick = { transport = t },
                                        label = { Text(t.uppercase()) },
                                        modifier = Modifier.padding(end = 8.dp)
                                    )
                                }
                            }

                            if (transport == "ws") {
                                OutlinedTextField(
                                    value = path, onValueChange = { path = it },
                                    label = { Text("WebSocket 路径") },
                                    placeholder = { Text("/ws") },
                                    modifier = Modifier.fillMaxWidth(),
                                    singleLine = true
                                )
                            }

                            Spacer(modifier = Modifier.height(8.dp))
                            OutlinedTextField(
                                value = sni, onValueChange = { sni = it },
                                label = { Text("伪装域名 (SNI)") },
                                placeholder = { Text("留空则使用Host") },
                                modifier = Modifier.fillMaxWidth(),
                                singleLine = true
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
