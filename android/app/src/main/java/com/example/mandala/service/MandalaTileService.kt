// 文件路径: android/app/src/main/java/com/example/mandala/service/MandalaTileService.kt

package com.example.mandala.service

import android.content.Context
import android.content.Intent
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.widget.Toast
import androidx.annotation.RequiresApi
import com.example.mandala.data.NodeRepository
import com.example.mandala.viewmodel.Node
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import mobile.Mobile
import java.io.File

@RequiresApi(Build.VERSION_CODES.N)
class MandalaTileService : TileService() {

    override fun onStartListening() {
        super.onStartListening()
        updateTileState()
    }

    override fun onClick() {
        super.onClick()
        
        // 1. 检查当前状态
        val isRunning = try { Mobile.isRunning() } catch (e: Exception) { false }

        if (isRunning) {
            // 2. 如果正在运行，则关闭
            stopVpn()
            // 点击后先更新为非活跃状态，等待实际停止回调（或下次下拉刷新）
            qsTile.state = Tile.STATE_INACTIVE
            qsTile.updateTile()
        } else {
            // 3. 如果未运行，则尝试启动
            // 需要异步读取配置，TileService 运行在主线程，IO操作需放入协程
            // 此时先将图标设为“不可用”状态防止重复点击，直到启动完成
            qsTile.state = Tile.STATE_UNAVAILABLE
            qsTile.updateTile()
            
            startVpnBackground()
        }
    }

    private fun updateTileState() {
        val tile = qsTile ?: return
        val isRunning = try { Mobile.isRunning() } catch (e: Exception) { false }
        
        tile.state = if (isRunning) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        tile.label = "Mandala"
        tile.updateTile()
    }

    private fun stopVpn() {
        val intent = Intent(this, MandalaVpnService::class.java).apply {
            action = MandalaVpnService.ACTION_STOP
        }
        startService(intent)
    }

    private fun startVpnBackground() {
        CoroutineScope(Dispatchers.IO).launch {
            try {
                // 读取节点
                val repository = NodeRepository(applicationContext)
                val nodes = repository.loadNodes()
                
                // 查找选中的节点，如果没有选中的，就用第一个
                val targetNode = nodes.find { it.isSelected } ?: nodes.firstOrNull()

                if (targetNode == null) {
                    withContext(Dispatchers.Main) {
                        Toast.makeText(applicationContext, "没有可用节点，请先进入 App 添加", Toast.LENGTH_SHORT).show()
                        updateTileState() // 恢复状态
                    }
                    return@launch
                }

                // 读取设置
                val prefs = getSharedPreferences("mandala_settings", Context.MODE_PRIVATE)
                val configJson = generateConfigJson(targetNode, prefs)

                // 启动 Service
                val intent = Intent(applicationContext, MandalaVpnService::class.java).apply {
                    action = MandalaVpnService.ACTION_START
                    putExtra(MandalaVpnService.EXTRA_CONFIG, configJson)
                }
                
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    startForegroundService(intent)
                } else {
                    startService(intent)
                }
                
                // UI 反馈
                withContext(Dispatchers.Main) {
                    // 假设启动成功，置为 Active
                    qsTile.state = Tile.STATE_ACTIVE
                    qsTile.updateTile()
                }

            } catch (e: Exception) {
                e.printStackTrace()
                withContext(Dispatchers.Main) {
                    Toast.makeText(applicationContext, "启动失败: ${e.message}", Toast.LENGTH_SHORT).show()
                    updateTileState()
                }
            }
        }
    }

    // 复用 MainViewModel 中的 JSON 生成逻辑
    private fun generateConfigJson(node: Node, prefs: android.content.SharedPreferences): String {
        val vpnMode = prefs.getBoolean("vpn_mode", true)
        val allowInsecure = prefs.getBoolean("allow_insecure", false)
        val tlsFragment = prefs.getBoolean("tls_fragment", true)
        val randomPadding = prefs.getBoolean("random_padding", false)
        val localPort = prefs.getInt("local_port", 10809)
        val loggingEnabled = prefs.getBoolean("logging_enabled", false)

        val useTls = node.sni.isNotEmpty() || node.transport == "ws" || node.port == 443
        
        val logPath = if (loggingEnabled) {
             val logDir = getExternalFilesDir(null)
             if (logDir != null) File(logDir, "mandala_core.log").absolutePath 
             else filesDir.absolutePath + "/mandala_core.log"
        } else ""

        // 注意：这里手动构建 JSON，需确保与 MainViewModel 逻辑一致
        return """
        {
            "tag": "${node.tag}",
            "type": "${node.protocol}",
            "server": "${node.server}",
            "server_port": ${node.port},
            "password": "${node.password}",
            "uuid": "${node.uuid}",
            "username": "${if(node.protocol == "socks5") node.uuid else ""}",
            "log_path": "$logPath",
            "tls": { "enabled": $useTls, "server_name": "${if (node.sni.isEmpty()) node.server else node.sni}", "insecure": $allowInsecure },
            "transport": { "type": "${node.transport}", "path": "${node.path}" },
            "settings": { "vpn_mode": $vpnMode, "fragment": $tlsFragment, "noise": $randomPadding },
            "local_port": $localPort
        }
        """.trimIndent()
    }
}
