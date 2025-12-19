package com.example.mandala.service

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import androidx.core.app.NotificationCompat
import com.example.mandala.MainActivity
import com.example.mandala.R
import mobile.Mobile

class MandalaVpnService : VpnService() {

    companion object {
        const val ACTION_START = "com.example.mandala.service.START"
        const val ACTION_STOP = "com.example.mandala.service.STOP"
        const val EXTRA_CONFIG = "config_json"
        
        private const val VPN_ADDRESS = "172.16.0.1"
        private const val CHANNEL_ID = "MandalaVpnChannel"
        private const val NOTIFICATION_ID = 1
    }

    private var vpnInterface: ParcelFileDescriptor? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopVpn()
            return START_NOT_STICKY
        }

        val config = intent?.getStringExtra(EXTRA_CONFIG) ?: ""
        startForeground(NOTIFICATION_ID, createNotification())
        startVpn(config)

        return START_STICKY
    }

    private fun startVpn(configJson: String) {
        if (vpnInterface != null) return

        try {
            val builder = Builder()
                .setSession("Mandala")
                .addAddress(VPN_ADDRESS, 24)
                .addRoute("0.0.0.0", 0)  // IPv4 全局
                .addRoute("::", 0)       // IPv6 全局
                .setMtu(1500)
                .addDnsServer("223.5.5.5")
                .addDisallowedApplication(packageName)

            vpnInterface = builder.establish()
            vpnInterface?.let {
                // 修复：fd.toLong() 和 mtu.toLong() 以匹配 Go 层生成的接口
                val err = Mobile.startVpn(it.fd.toLong(), 1500L, configJson)
                if (err.isNotEmpty()) {
                    Log.e("MandalaVPN", "核心启动失败: $err")
                    stopVpn()
                }
            }
        } catch (e: Exception) {
            Log.e("MandalaVPN", "建立 VPN 接口失败", e)
            stopVpn()
        }
    }

    private fun stopVpn() {
        Mobile.stop()
        vpnInterface?.close()
        vpnInterface = null
        stopForeground(true)
        stopSelf()
    }

    override fun onDestroy() {
        stopVpn()
        super.onDestroy()
    }

    private fun createNotification(): android.app.Notification {
        val manager = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(CHANNEL_ID, "VPN 状态", NotificationManager.IMPORTANCE_LOW)
            manager.createNotificationChannel(channel)
        }
        val intent = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("Mandala VPN")
            .setContentText("正在运行中...")
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentIntent(intent)
            .setOngoing(true)
            .build()
    }
}
