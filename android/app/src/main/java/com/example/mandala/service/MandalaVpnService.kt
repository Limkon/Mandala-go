// 文件路径: android/app/src/main/java/com/example/mandala/service/MandalaVpnService.kt

package com.example.mandala.service

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.core.app.NotificationCompat
import com.example.mandala.MainActivity
import com.example.mandala.R
import mobile.Mobile
import java.io.IOException

class MandalaVpnService : VpnService() {
    companion object {
        const val ACTION_START = "com.example.mandala.service.START"
        const val ACTION_STOP = "com.example.mandala.service.STOP"
        const val ACTION_VPN_STOPPED = "com.example.mandala.service.VPN_STOPPED"
        
        const val EXTRA_CONFIG = "config_json"
        private const val VPN_ADDRESS = "172.16.0.1"
        private const val CHANNEL_ID = "MandalaChannel"
        private const val NOTIFICATION_ID = 1
    }

    private var vpnInterface: ParcelFileDescriptor? = null
    
    @Volatile
    private var isRunning = false

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopVpn()
                return START_NOT_STICKY
            }
            ACTION_START -> {
                val config = intent.getStringExtra(EXTRA_CONFIG) ?: ""
                if (config.isEmpty()) {
                    stopVpn()
                } else {
                    startVpn(config)
                }
            }
        }
        return START_NOT_STICKY
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "Mandala VPN 状态",
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "显示 VPN 连接状态"
            }
            val manager = getSystemService(NotificationManager::class.java)
            manager?.createNotificationChannel(channel)
        }
    }

    private fun createNotification(content: String): android.app.Notification {
        val pendingIntent = Intent(this, MainActivity::class.java).let {
            PendingIntent.getActivity(this, 0, it, PendingIntent.FLAG_IMMUTABLE)
        }

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("Mandala VPN")
            .setContentText(content)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .build()
    }

    @Synchronized
    private fun startVpn(configJson: String) {
        if (isRunning) {
            android.util.Log.d("MandalaVpn", "VPN 已经在运行中，跳过重复启动")
            return
        }

        try {
            // 提升为前台服务
            startForeground(NOTIFICATION_ID, createNotification("正在初始化核心..."))

            // 清理旧接口防止 FD 泄露
            vpnInterface?.let {
                try { it.close() } catch (e: Exception) {}
                vpnInterface = null
            }

            val builder = Builder()
                .addAddress(VPN_ADDRESS, 24)
                .addRoute("0.0.0.0", 0)
                .addRoute("::", 0)
                .setMtu(1500)
                .addDnsServer("8.8.8.8")
                .addDisallowedApplication(packageName)
                .setSession("Mandala Core")
            
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                builder.setMetered(false)
            }

            vpnInterface = builder.establish()
            val fd = vpnInterface?.fd

            if (fd == null) {
                android.util.Log.e("MandalaVpn", "无法建立 VPN 接口 (establish returned null)")
                stopVpn()
                return
            }

            // 标记为运行中
            isRunning = true

            // 启动 Go 核心
            val err = Mobile.startVpn(fd.toLong(), 1500L, configJson)
            if (err.isNotEmpty()) {
                android.util.Log.e("MandalaVpn", "Go 核心启动失败: $err")
                stopVpn()
                return
            }

            // 更新通知状态
            val manager = getSystemService(NotificationManager::class.java)
            manager?.notify(NOTIFICATION_ID, createNotification("VPN 已连接"))

        } catch (e: Exception) {
            android.util.Log.e("MandalaVpn", "启动流程发生异常: ${e.message}")
            e.printStackTrace()
            stopVpn()
        }
    }

    @Synchronized
    private fun stopVpn() {
        if (!isRunning && vpnInterface == null) return
        
        isRunning = false
        
        try {
            Mobile.stop()
        } catch (e: Exception) {
            android.util.Log.e("MandalaVpn", "停止核心异常: ${e.message}")
        }
        
        try {
            vpnInterface?.close()
        } catch (e: IOException) {
            e.printStackTrace()
        } finally {
            vpnInterface = null
        }
        
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
        
        stopSelf()

        // 发送广播同步 UI 状态
        val intent = Intent(ACTION_VPN_STOPPED).setPackage(packageName)
        sendBroadcast(intent)
        android.util.Log.d("MandalaVpn", "VPN 服务已彻底停止")
    }

    override fun onDestroy() {
        stopVpn()
        super.onDestroy()
    }

    override fun onRevoke() {
        stopVpn()
        super.onRevoke()
    }
}
