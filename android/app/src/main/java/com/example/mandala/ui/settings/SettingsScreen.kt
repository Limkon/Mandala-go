// 文件路径: android/app/src/main/java/com/example/mandala/ui/settings/SettingsScreen.kt

package com.example.mandala.ui.settings

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

    var showPortDialog by remember { mutableStateOf(false) }
    var showFragmentDialog by remember { mutableStateOf(false) } // [新增]
    var showNoiseDialog by remember { mutableStateOf(false) }   // [新增]

    // --- 弹窗逻辑集锦 ---

    if (showPortDialog) {
        PortEditDialog(
            title = strings.localPort,
            currentValue = localPort.toString(),
            range = 1024..65535,
            onConfirm = { viewModel.updateLocalPort(it) },
            onDismiss = { showPortDialog = false },
            strings = strings
        )
    }

    if (showFragmentDialog) {
        PortEditDialog(
            title = strings.fragmentSize,
            currentValue = fragmentSize.toString(),
            range = 1..500,
            onConfirm = { viewModel.updateFragmentSize(it) },
            onDismiss = { showFragmentDialog = false },
            strings = strings
        )
    }

    if (showNoiseDialog) {
        PortEditDialog(
            title = strings.noiseSize,
            currentValue = noiseSize.toString(),
            range = 1..2000,
            onConfirm = { viewModel.updateNoiseSize(it) },
            onDismiss = { showNoiseDialog = false },
            strings = strings
        )
    }

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
            SwitchSetting(
                title = strings.tlsFragment,
                subtitle = strings.tlsFragmentDesc,
                checked = tlsFragment,
                onCheckedChange = { viewModel.updateSetting("tls_fragment", it) }
            )
            // [新增] 自定义分片大小入口
            if (tlsFragment) {
                ClickableSetting(
                    title = strings.fragmentSize,
                    value = fragmentSize.toString(),
                    icon = Icons.Default.Edit,
                    onClick = { showFragmentDialog = true }
                )
            }

            SwitchSetting(
                title = strings.randomPadding,
                subtitle = strings.randomPaddingDesc,
                checked = randomPadding,
                onCheckedChange = { viewModel.updateSetting("random_padding", it) }
            )
            // [新增] 自定义填充大小入口
            if (randomPadding) {
                ClickableSetting(
                    title = strings.noiseSize,
                    value = noiseSize.toString(),
                    icon = Icons.Default.Edit,
                    onClick = { showNoiseDialog = true }
                )
            }

            SwitchSetting(
                title = strings.enableLogging,
                subtitle = strings.enableLoggingDesc,
                checked = loggingEnabled,
                onCheckedChange = { viewModel.updateSetting("logging_enabled", it) }
            )
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
            Text("Mandala Client v1.1.0", style = MaterialTheme.typography.bodyMedium, color = Color.Gray)
            Text("Core: Go 1.23 / Gomobile", style = MaterialTheme.typography.bodyMedium, color = Color.Gray)
        }
    }
}

// --- 通用数值编辑弹窗组件 ---
@Composable
fun PortEditDialog(
    title: String,
    currentValue: String,
    range: IntRange,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
    strings: com.example.mandala.viewmodel.AppStrings
) {
    var tempVal by remember { mutableStateOf(currentValue) }
    var isError by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column {
                OutlinedTextField(
                    value = tempVal,
                    onValueChange = { 
                        tempVal = it.filter { char -> char.isDigit() }
                        isError = false
                    },
                    label = { Text("范围: ${range.first} - ${range.last}") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    isError = isError,
                    singleLine = true
                )
                if (isError) {
                    Text("输入无效", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val v = tempVal.toIntOrNull()
                if (v != null && v in range) {
                    onConfirm(tempVal)
                    onDismiss()
                } else {
                    isError = true
                }
            }) { Text(strings.confirm) }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(strings.cancel) }
        }
    )
}

// --- 其余 UI 封装组件保持不变 ---

@Composable
fun SettingSection(title: String, content: @Composable ColumnScope.() -> Unit) {
    Text(title, color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
    Spacer(modifier = Modifier.height(8.dp))
    Card(modifier = Modifier.fillMaxWidth(), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)) {
        Column(modifier = Modifier.padding(16.dp)) { content() }
    }
    Spacer(modifier = Modifier.height(24.dp))
}

@Composable
fun SwitchSetting(title: String, subtitle: String, checked: Boolean, onCheckedChange: (Boolean) -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
        Column(modifier = Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = Color.Gray)
        }
        Switch(checked = checked, onCheckedChange = onCheckedChange)
    }
}

@Composable
fun ClickableSetting(title: String, value: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 12.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
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
    Row(modifier = Modifier.fillMaxWidth().clickable { expanded = true }.padding(vertical = 12.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        Box {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(currentValue, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                Icon(com.example.mandala.MainActivity().let { Icons.Default.ArrowDropDown }, contentDescription = null)
            }
            DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                options.forEachIndexed { index, label ->
                    DropdownMenuItem(text = { Text(label) }, onClick = { onOptionSelected(index); expanded = false })
                }
            }
        }
    }
}
