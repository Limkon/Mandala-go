package com.example.mandala.ui.home

import androidx.compose.animation.core.*
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PowerSettingsNew
import androidx.compose.material.icons.filled.QrCodeScanner
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.example.mandala.viewmodel.MainViewModel

@Composable
fun HomeScreen(viewModel: MainViewModel) {
    val isConnected by viewModel.isConnected.collectAsState()
    val currentNode by viewModel.currentNode.collectAsState()
    val logs by viewModel.logs.collectAsState()
    // [新增] 获取多语言字符串
    val strings by viewModel.appStrings.collectAsState()

    val infiniteTransition = rememberInfiniteTransition(label = "spin_transition")
    val angle by infiniteTransition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(
            animation = tween(2000, easing = LinearEasing)
        ), label = "spin_angle"
    )

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        // --- 顶部栏 ---
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            Text(
                "Mandala",
                fontSize = 28.sp, 
                fontWeight = FontWeight.Bold, 
                color = MaterialTheme.colorScheme.primary
            )
            IconButton(onClick = { /* TODO: 扫码 */ }) {
                Icon(Icons.Default.QrCodeScanner, contentDescription = "Scan")
            }
        }

        Spacer(modifier = Modifier.height(32.dp))

        // --- 状态卡片 ---
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
            shape = RoundedCornerShape(16.dp)
        ) {
            Column(modifier = Modifier.padding(20.dp)) {
                // 使用 strings.home 或其他合适标签，这里暂用 currentNode 的上一级标题
                Text("Current Node", style = MaterialTheme.typography.labelSmall, color = Color.Gray)
                Spacer(modifier = Modifier.height(4.dp))
                Text(
                    currentNode.tag, 
                    style = MaterialTheme.typography.titleLarge, 
                    fontWeight = FontWeight.Bold
                )
                Spacer(modifier = Modifier.height(12.dp))
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween
                ) {
                    Text(
                        if (isConnected) strings.connected else strings.notConnected,
                        color = if (isConnected) Color(0xFF4CAF50) else Color.Gray,
                        fontWeight = FontWeight.Bold
                    )
                    Text(currentNode.protocol.uppercase(), style = MaterialTheme.typography.labelMedium)
                }
            }
        }

        Spacer(modifier = Modifier.weight(1f))

        // --- 连接按钮 ---
        Box(contentAlignment = Alignment.Center) {
            if (isConnected) {
                Box(
                    modifier = Modifier
                        .size(180.dp)
                        .rotate(angle)
                        .background(
                            brush = Brush.sweepGradient(
                                listOf(MaterialTheme.colorScheme.primary.copy(alpha = 0.3f), Color.Transparent)
                            ),
                            shape = CircleShape
                        )
                )
            }

            Button(
                onClick = { viewModel.toggleConnection() },
                modifier = Modifier.size(140.dp),
                shape = CircleShape,
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (isConnected) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
                ),
                elevation = ButtonDefaults.buttonElevation(8.dp)
            ) {
                Icon(
                    Icons.Default.PowerSettingsNew,
                    contentDescription = if (isConnected) strings.disconnect else strings.connect,
                    modifier = Modifier.size(64.dp).scale(1.2f)
                )
            }
        }

        Spacer(modifier = Modifier.weight(1f))

        // --- 日志预览 ---
        Surface(
            modifier = Modifier
                .fillMaxWidth()
                .height(120.dp),
            shape = RoundedCornerShape(12.dp),
            color = MaterialTheme.colorScheme.surface.copy(alpha = 0.5f),
            border = androidx.compose.foundation.BorderStroke(1.dp, Color.Gray.copy(alpha = 0.2f))
        ) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("System Logs", style = MaterialTheme.typography.labelSmall, color = Color.Gray)
                Spacer(modifier = Modifier.height(8.dp))
                logs.takeLast(4).forEach { log ->
                    Text(
                        log, 
                        style = MaterialTheme.typography.bodySmall, 
                        maxLines = 1,
                        fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace
                    )
                }
            }
        }
    }
}
