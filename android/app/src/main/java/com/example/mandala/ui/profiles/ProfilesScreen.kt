package com.example.mandala.ui.profiles

import android.content.ClipboardManager
import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ContentPaste
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.example.mandala.viewmodel.MainViewModel
import com.example.mandala.viewmodel.Node
import com.example.mandala.viewmodel.AppStrings

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProfilesScreen(viewModel: MainViewModel) {
    val nodes by viewModel.nodes.collectAsState()
    val currentNode by viewModel.currentNode.collectAsState()
    val strings by viewModel.appStrings.collectAsState()
    val context = LocalContext.current

    // [新增] 弹窗状态管理
    var nodeToEdit by remember { mutableStateOf<Node?>(null) }
    var nodeToDelete by remember { mutableStateOf<Node?>(null) }

    // [新增] 编辑弹窗逻辑
    if (nodeToEdit != null) {
        EditNodeDialog(
            node = nodeToEdit!!,
            strings = strings,
            onDismiss = { nodeToEdit = null },
            onSave = { updatedNode ->
                viewModel.updateNode(nodeToEdit!!, updatedNode)
                nodeToEdit = null
            }
        )
    }

    // [新增] 删除确认弹窗逻辑
    if (nodeToDelete != null) {
        AlertDialog(
            onDismissRequest = { nodeToDelete = null },
            title = { Text(strings.delete) },
            text = { Text(strings.deleteConfirm) },
            confirmButton = {
                TextButton(
                    onClick = {
                        viewModel.deleteNode(nodeToDelete!!)
                        nodeToDelete = null
                    },
                    colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)
                ) { Text(strings.delete) }
            },
            dismissButton = {
                TextButton(onClick = { nodeToDelete = null }) { Text(strings.cancel) }
            }
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(strings.nodeManagement) },
                actions = {
                    IconButton(onClick = {
                        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                        val clipData = clipboard.primaryClip
                        if (clipData != null && clipData.itemCount > 0) {
                            val text = clipData.getItemAt(0).text.toString()
                            viewModel.importFromText(text) { success, message ->
                                Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
                            }
                        } else {
                            Toast.makeText(context, strings.clipboardEmpty, Toast.LENGTH_SHORT).show()
                        }
                    }) {
                        Icon(Icons.Default.ContentPaste, contentDescription = strings.importFromClipboard)
                    }
                }
            )
        }
    ) { padding ->
        if (nodes.isEmpty()) {
            Box(modifier = Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                Text(strings.importFromClipboard + "...", style = MaterialTheme.typography.bodyLarge)
            }
        } else {
            LazyColumn(modifier = Modifier.fillMaxSize().padding(padding)) {
                items(nodes) { node ->
                    NodeItem(
                        node = node,
                        isSelected = node.server == currentNode.server && node.port == currentNode.port,
                        strings = strings,
                        onSelect = { viewModel.selectNode(node) },
                        onEdit = { nodeToEdit = node },
                        onDelete = { nodeToDelete = node }
                    )
                }
            }
        }
    }
}

// [修改] NodeItem 增加操作菜单
@Composable
fun NodeItem(
    node: Node, 
    isSelected: Boolean, 
    strings: AppStrings,
    onSelect: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit
) {
    var showMenu by remember { mutableStateOf(false) }

    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .clickable { onSelect() },
        colors = CardDefaults.cardColors(
            containerColor = if (isSelected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant
        )
    ) {
        Row(
            modifier = Modifier
                .padding(16.dp)
                .fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically
        ) {
            // 左侧信息
            Column(modifier = Modifier.weight(1f)) {
                Text(text = node.tag, style = MaterialTheme.typography.titleMedium)
                Text(
                    text = "${node.protocol.uppercase()} | ${node.server}:${node.port}",
                    style = MaterialTheme.typography.bodySmall
                )
            }
            
            // 右侧图标与菜单
            if (isSelected) {
                Icon(
                    Icons.Default.Check,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(end = 8.dp)
                )
            }

            // [新增] 更多操作按钮
            Box {
                IconButton(onClick = { showMenu = true }) {
                    Icon(Icons.Default.MoreVert, contentDescription = "More")
                }
                DropdownMenu(
                    expanded = showMenu,
                    onDismissRequest = { showMenu = false }
                ) {
                    DropdownMenuItem(
                        text = { Text(strings.edit) },
                        onClick = {
                            showMenu = false
                            onEdit()
                        }
                    )
                    DropdownMenuItem(
                        text = { Text(strings.delete, color = MaterialTheme.colorScheme.error) },
                        onClick = {
                            showMenu = false
                            onDelete()
                        }
                    )
                }
            }
        }
    }
}

// [新增] 编辑节点弹窗组件
@Composable
fun EditNodeDialog(
    node: Node,
    strings: AppStrings,
    onDismiss: () -> Unit,
    onSave: (Node) -> Unit
) {
    // 临时状态用于表单编辑
    var tag by remember { mutableStateOf(node.tag) }
    var server by remember { mutableStateOf(node.server) }
    var port by remember { mutableStateOf(node.port.toString()) }
    var password by remember { mutableStateOf(node.password) }
    var uuid by remember { mutableStateOf(node.uuid) }
    var sni by remember { mutableStateOf(node.sni) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(strings.edit) },
        text = {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
            ) {
                OutlinedTextField(
                    value = tag, onValueChange = { tag = it },
                    label = { Text(strings.tag) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp)
                )
                OutlinedTextField(
                    value = server, onValueChange = { server = it },
                    label = { Text(strings.address) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp)
                )
                OutlinedTextField(
                    value = port, onValueChange = { port = it.filter { char -> char.isDigit() } },
                    label = { Text(strings.port) },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp)
                )
                
                // 根据协议类型显示不同的鉴权字段
                if (node.protocol == "vless" || node.protocol == "vmess") {
                    OutlinedTextField(
                        value = uuid, onValueChange = { uuid = it },
                        label = { Text(strings.uuid) },
                        modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp)
                    )
                } else {
                    OutlinedTextField(
                        value = password, onValueChange = { password = it },
                        label = { Text(strings.password) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp)
                    )
                }

                OutlinedTextField(
                    value = sni, onValueChange = { sni = it },
                    label = { Text(strings.sni) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val newPort = port.toIntOrNull() ?: 443
                val updatedNode = node.copy(
                    tag = tag,
                    server = server,
                    port = newPort,
                    password = password,
                    uuid = uuid,
                    sni = sni
                )
                onSave(updatedNode)
            }) { Text(strings.save) }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(strings.cancel) }
        }
    )
}
