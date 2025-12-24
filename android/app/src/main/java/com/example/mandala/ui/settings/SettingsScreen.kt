// 文件路径: android/app/src/main/java/com/example/mandala/ui/settings/SettingsScreen.kt

package com.example.mandala.ui.settings

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.example.mandala.viewmodel.AppLanguage
import com.example.mandala.viewmodel.AppThemeMode
import com.example.mandala.viewmodel.MainViewModel

@Composable
fun SettingsScreen(viewModel: MainViewModel) {
    // 收集状态
    val strings by viewModel.appStrings.collectAsState()
    val vpnMode by viewModel.vpnMode.collectAsState()
    val allowInsecure by viewModel.allowInsecure.collectAsState()
    val tlsFragment by viewModel.tlsFragment.collectAsState()
    val fragmentSize by viewModel.fragmentSize.collectAsState() // [新增]
    val randomPadding by viewModel.randomPadding.collectAsState()
    val noiseSize by viewModel.noiseSize.collectAsState() // [新增]
    val localPort by viewModel.localPort.collectAsState()
    val loggingEnabled by viewModel.loggingEnabled.collectAsState()
    val themeMode by viewModel.themeMode.collectAsState()
    val language by viewModel.language.collectAsState()

    // 弹窗状态
    var showPortDialog by remember { mutableStateOf(false) }
    var showFragmentDialog by remember { mutableStateOf(false) } // [新增]
    var showNoiseDialog by remember { mutableStateOf(false) }    // [新增]

    // --- 弹窗逻辑 ---

    // 1. 端口编辑弹窗
    if (showPortDialog) {
        EditNumberDialog(
            title = strings.localPort,
            initialValue = localPort.toString(),
            onConfirm = { viewModel.updateLocalPort(it); showPortDialog = false },
            onDismiss = { showPortDialog = false },
            range = 1024..65535,
            confirmText = strings.confirm,
            cancelText = strings.cancel
        )
    }

    // 2. [新增] 分片大小编辑弹窗
    if (showFragmentDialog) {
        EditNumberDialog(
            title = strings.fragmentSize,
            initialValue = fragmentSize.toString(),
            onConfirm = { viewModel.updateFragmentSize(it); showFragmentDialog = false },
            onDismiss = { showFragmentDialog = false },
            range = 1..10000, // 合理范围
            confirmText = strings.confirm,
            cancelText = strings.cancel
        )
    }

    // 3. [新增] 填充大小编辑弹窗
    if (showNoiseDialog) {
        EditNumberDialog(
            title = strings.noiseSize,
            initialValue = noiseSize.toString(),
            onConfirm = { viewModel.updateNoiseSize(it); showNoiseDialog = false },
            onDismiss = { showNoiseDialog = false },
            range = 0..10000,
            confirmText = strings.confirm,
            cancelText = strings.cancel
        )
    }

    // --- 界面布局 ---

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
            .verticalScroll(rememberScrollState())
    ) {
        Text(strings.settings, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Spacer(modifier = Modifier.height(24.dp))

        // --- 连接设置 ---
        SettingSection(strings.connectionSettings) {
            SwitchSetting(
                title = strings.vpnMode,
                subtitle = strings.vpnModeDesc,
                checked = vpnMode,
                onCheckedChange = { viewModel.updateSetting("vpn_mode", it) }
            )
            SwitchSetting(
                title = strings.allowInsecure,
                subtitle = strings.allowInsecureDesc,
                checked = allowInsecure,
                onCheckedChange = { viewModel.updateSetting("allow_insecure", it) }
            )
        }

        // --- 协议参数 ---
        SettingSection(strings.protocolSettings) {
            // TLS 分片设置
            SwitchSetting(
                title = strings.tlsFragment,
                subtitle = strings.tlsFragmentDesc,
                checked = tlsFragment,
                onCheckedChange = { viewModel.updateSetting("tls_fragment", it) }
            )
            // [新增] 仅在开启分片时显示大小设置
            AnimatedVisibility(visible = tlsFragment) {
                ClickableSetting(
                    title = strings.fragmentSize,
                    value = "$fragmentSize Byte",
                    icon = Icons.Default.Edit,
                    onClick = { showFragmentDialog = true }
                )
            }

            Divider(modifier = Modifier.padding(vertical = 8.dp), color = Color.LightGray.copy(alpha = 0.3f))

            // 随机填充设置
            SwitchSetting(
                title = strings.randomPadding,
                subtitle = strings.randomPaddingDesc,
                checked = randomPadding,
                onCheckedChange = { viewModel.updateSetting("random_padding", it) }
            )
            // [新增] 仅在开启填充时显示大小设置
            AnimatedVisibility(visible = randomPadding) {
                ClickableSetting(
                    title = strings.noiseSize,
                    value = "$noiseSize Byte",
                    icon = Icons.Default.Edit,
                    onClick = { showNoiseDialog = true }
                )
            }

            Divider(modifier = Modifier.padding(vertical = 8.dp), color = Color.LightGray.copy(alpha = 0.3f))

            // 日志开关
            SwitchSetting(
                title = strings.enableLogging,
                subtitle = strings.enableLoggingDesc,
                checked = loggingEnabled,
                onCheckedChange = { viewModel.updateSetting("logging_enabled", it) }
            )
            
            // 本地端口
            ClickableSetting(
                title = strings.localPort,
                value = localPort.toString(),
                icon = Icons.Default.Edit,
                onClick = { showPortDialog = true }
            )
        }

        // --- 应用设置 (主题与语言) ---
        SettingSection(strings.appSettings) {
            DropdownSetting(
                title = strings.theme,
                currentValue = when(themeMode) {
                    AppThemeMode.SYSTEM -> "系统默认"
                    AppThemeMode.LIGHT -> "浅色"
                    AppThemeMode.DARK -> "深色"
                },
                options = listOf("系统默认", "浅色", "深色"),
                onOptionSelected = { index ->
                    viewModel.updateTheme(AppThemeMode.values()[index])
                }
            )

            DropdownSetting(
                title = strings.language,
                currentValue = when(language) {
                    AppLanguage.CHINESE -> "简体中文"
                    AppLanguage.ENGLISH -> "English"
                },
                options = listOf("简体中文", "English"),
                onOptionSelected = { index ->
                    viewModel.updateLanguage(AppLanguage.values()[index])
                }
            )
        }

        // --- 关于 ---
        SettingSection(strings.about) {
            Text(
                "Mandala Client v1.2.0", // 版本号小幅升级
                style = MaterialTheme.typography.bodyMedium,
                color = Color.Gray
            )
            Text(
                "Core: Go 1.23 / Gomobile",
                style = MaterialTheme.typography.bodyMedium,
                color = Color.Gray
            )
        }
    }
}

// --- 组件封装 ---

// [新增] 通用数字编辑弹窗
@Composable
fun EditNumberDialog(
    title: String,
    initialValue: String,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
    range: IntRange,
    confirmText: String,
    cancelText: String
) {
    var tempValue by remember { mutableStateOf(initialValue) }
    var isError by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column {
                OutlinedTextField(
                    value = tempValue,
                    onValueChange = { 
                        tempValue = it.filter { char -> char.isDigit() }
                        isError = false
                    },
                    label = { Text("Value (${range.first}-${range.last})") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    isError = isError,
                    singleLine = true
                )
                if (isError) {
                    Text("数值无效", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val p = tempValue.toIntOrNull()
                if (p != null && p in range) {
                    onConfirm(tempValue)
                } else {
                    isError = true
                }
            }) { Text(confirmText) }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(cancelText) }
        }
    )
}

@Composable
fun SettingSection(title: String, content: @Composable ColumnScope.() -> Unit) {
    Text(
        title,
        color = MaterialTheme.colorScheme.primary,
        style = MaterialTheme.typography.titleSmall,
        fontWeight = FontWeight.Bold
    )
    Spacer(modifier = Modifier.height(8.dp))
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            content()
        }
    }
    Spacer(modifier = Modifier.height(24.dp))
}

@Composable
fun SwitchSetting(title: String, subtitle: String, checked: Boolean, onCheckedChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = Color.Gray)
        }
        Switch(checked = checked, onCheckedChange = onCheckedChange)
    }
}

@Composable
fun ClickableSetting(title: String, value: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(value, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Bold, color = Color.Gray)
            Spacer(modifier = Modifier.width(8.dp))
            Icon(icon, contentDescription = null, tint = Color.Gray, modifier = Modifier.size(20.dp))
        }
    }
}

@Composable
fun DropdownSetting(title: String, currentValue: String, options: List<String>, onOptionSelected: (Int) -> Unit) {
    var expanded by remember { mutableStateOf(false) }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { expanded = true }
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        
        Box {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(currentValue, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                Icon(Icons.Default.ArrowDropDown, contentDescription = null)
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false }
            ) {
                options.forEachIndexed { index, label ->
                    DropdownMenuItem(
                        text = { Text(label) },
                        onClick = {
                            onOptionSelected(index)
                            expanded = false
                        }
                    )
                }
            }
        }
    }
}
