// 文件路径: android/app/src/main/java/com/example/mandala/ui/settings/SettingsScreen.kt

package com.example.mandala.ui.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.example.mandala.viewmodel.MainViewModel

@Composable
fun SettingsScreen(viewModel: MainViewModel) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
            .verticalScroll(rememberScrollState())
    ) {
        Text("设置", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Spacer(modifier = Modifier.height(24.dp))

        // 连接设置
        SettingSection("连接设置") {
            SwitchSetting(
                title = "VPN 模式",
                subtitle = "通过 Mandala 路由所有设备流量",
                initialState = true
            )
            SwitchSetting(
                title = "允许不安全连接",
                subtitle = "跳过 TLS 证书验证 (危险)",
                initialState = false
            )
        }

        // Mandala 协议设置
        SettingSection("协议参数 (核心)") {
            SwitchSetting(
                title = "TLS 分片",
                subtitle = "拆分 TLS 记录以绕过 DPI 检测",
                initialState = true
            )
            SwitchSetting(
                title = "随机填充",
                subtitle = "向数据包添加随机噪音",
                initialState = false
            )
            TextSetting(
                title = "本地监听端口",
                value = "10809"
            )
        }

        // 关于
        SettingSection("关于") {
            Text(
                "Mandala 客户端 v1.0.0",
                style = MaterialTheme.typography.bodyMedium,
                color = Color.Gray
            )
            Text(
                "核心版本: Go 1.21 (Gomobile)",
                style = MaterialTheme.typography.bodyMedium,
                color = Color.Gray
            )
        }
    }
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
fun SwitchSetting(title: String, subtitle: String, initialState: Boolean) {
    var checked by remember { mutableStateOf(initialState) }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = Color.Gray)
        }
        Switch(checked = checked, onCheckedChange = { checked = it })
    }
}

@Composable
fun TextSetting(title: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        Text(value, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Bold, color = Color.Gray)
    }
}
