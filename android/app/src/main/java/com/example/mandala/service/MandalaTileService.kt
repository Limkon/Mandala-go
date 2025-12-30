// 文件路径: android/app/src/main/java/com/example/mandala/service/MandalaTileService.kt

package com.example.mandala.service

import android.content.Intent
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.annotation.RequiresApi
import com.example.mandala.MainActivity
import mobile.Mobile

@RequiresApi(Build.VERSION_CODES.N)
class MandalaTileService : TileService() {

    // 当用户拉下通知栏看到该图标时调用
    override fun onStartListening() {
        super.onStartListening()
        updateTileState()
    }

    // 当用户点击该图标时调用
    override fun onClick() {
        super.onClick()
        
        // 点击动作：收起通知栏并启动主 App
        val intent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        
        // startActivityAndCollapse 是 TileService 专用的启动方法
        startActivityAndCollapse(intent)
    }

    private fun updateTileState() {
        val tile = qsTile ?: return
        
        // 检查 VPN 核心是否在运行
        // Mobile.isRunning() 来自您的 Go 核心库接口
        val isRunning = try {
            Mobile.isRunning()
        } catch (e: Exception) {
            false
        }

        // 设置状态：Active(高亮) 表示已连接，Inactive(灰色) 表示未连接
        tile.state = if (isRunning) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        
        // 设置显示的文字
        tile.label = "Mandala"
        
        // 应用更新
        tile.updateTile()
    }
}
