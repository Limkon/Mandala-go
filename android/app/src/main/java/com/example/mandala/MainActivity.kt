package com.example.mandala

import android.app.Activity
import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.example.mandala.service.MandalaVpnService
import com.example.mandala.ui.home.HomeScreen
import com.example.mandala.ui.profiles.ProfilesScreen
import com.example.mandala.ui.settings.SettingsScreen
import com.example.mandala.ui.theme.MandalaTheme
import com.example.mandala.viewmodel.MainViewModel
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {

    private var pendingConfigJson: String? = null

    // 注册 VPN 权限请求回调
    // 当 VpnService.prepare() 返回 Intent 时，需要使用此 Launcher 启动系统授权弹窗
    private val vpnPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            // 用户点击了“确定”，同意授权，现在可以启动服务了
            pendingConfigJson?.let { startVpnService(it) }
        } else {
            Toast.makeText(this, "需要 VPN 权限才能连接", Toast.LENGTH_SHORT).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        setContent {
            MandalaTheme {
                // 在 Compose 中获取 ViewModel (Factory 会自动处理 AndroidViewModel)
                val viewModel: MainViewModel = viewModel()
                
                // 监听 ViewModel 发出的 VPN 事件 (启动/停止)
                LaunchedEffect(Unit) {
                    lifecycleScope.launch {
                        repeatOnLifecycle(Lifecycle.State.STARTED) {
                            viewModel.vpnEvent.collect { event ->
                                when (event) {
                                    is MainViewModel.VpnEvent.StartVpn -> prepareAndStartVpn(event.configJson)
                                    is MainViewModel.VpnEvent.StopVpn -> stopVpnService()
                                }
                            }
                        }
                    }
                }

                // 加载主界面 UI
                MainApp(viewModel)
            }
        }
    }

    // 准备启动 VPN：先检查是否有权限
    private fun prepareAndStartVpn(configJson: String) {
        pendingConfigJson = configJson
        
        // 检查系统 VPN 权限，如果返回 null 表示已经有权限
        val intent = VpnService.prepare(this)
        if (intent != null) {
            // 需要请求权限，弹出系统对话框
            vpnPermissionLauncher.launch(intent)
        } else {
            // 已有权限，直接启动服务
            startVpnService(configJson)
        }
    }

    // 实际启动 VPN 服务 (前台服务)
    private fun startVpnService(configJson: String) {
        val intent = Intent(this, MandalaVpnService::class.java).apply {
            action = MandalaVpnService.ACTION_START
            putExtra(MandalaVpnService.EXTRA_CONFIG, configJson)
        }
        // Android 8.0+ 必须使用 startForegroundService 启动后台常驻服务
        startForegroundService(intent)
        
        // 通知 ViewModel 更新连接状态
        // 实际项目中建议通过 BroadcastReceiver 或 AIDL 通信，这里简化处理
        val viewModel: MainViewModel = androidx.lifecycle.ViewModelProvider(this)[MainViewModel::class.java]
        viewModel.onVpnStarted()
    }

    // 停止 VPN 服务
    private fun stopVpnService() {
        val intent = Intent(this, MandalaVpnService::class.java).apply {
            action = MandalaVpnService.ACTION_STOP
        }
        startService(intent)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainApp(viewModel: MainViewModel) {
    val navController = rememberNavController()
    
    // 底部导航配置 - 使用中文标签
    // Triple: (Label, Route, Icon)
    val navItems = listOf(
        Triple("首页", "Home", Icons.Filled.Home),
        Triple("节点", "Profiles", Icons.Filled.List),
        Triple("设置", "Settings", Icons.Filled.Settings)
    )

    Scaffold(
        bottomBar = {
            NavigationBar {
                val navBackStackEntry by navController.currentBackStackEntryAsState()
                val currentRoute = navBackStackEntry?.destination?.route

                navItems.forEach { (label, route, icon) ->
                    NavigationBarItem(
                        icon = { Icon(icon, contentDescription = label) },
                        label = { Text(label) },
                        selected = currentRoute == route,
                        onClick = {
                            navController.navigate(route) {
                                // 避免堆叠过多页面
                                popUpTo(navController.graph.startDestinationId) {
                                    saveState = true
                                }
                                launchSingleTop = true
                                restoreState = true
                            }
                        }
                    )
                }
            }
        }
    ) { innerPadding ->
        // 导航主机
        NavHost(
            navController = navController,
            startDestination = "Home", // 路由 Key 保持英文
            modifier = Modifier.padding(innerPadding)
        ) {
            composable("Home") { HomeScreen(viewModel) }
            composable("Profiles") { ProfilesScreen(viewModel) }
            composable("Settings") { SettingsScreen(viewModel) }
        }
    }
}
