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
    private val vpnPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            // 用户同意授权，启动服务
            pendingConfigJson?.let { startVpnService(it) }
        } else {
            Toast.makeText(this, "需要 VPN 权限才能连接", Toast.LENGTH_SHORT).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        setContent {
            MandalaTheme {
                // 在 Compose 中获取 ViewModel (Factory 自动处理 AndroidViewModel)
                val viewModel: MainViewModel = viewModel()
                
                // 监听 VPN 事件
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

                MainApp(viewModel)
            }
        }
    }

    private fun prepareAndStartVpn(configJson: String) {
        pendingConfigJson = configJson
        
        // 检查系统 VPN 权限
        val intent = VpnService.prepare(this)
        if (intent != null) {
            // 需要请求权限，弹出系统对话框
            vpnPermissionLauncher.launch(intent)
        } else {
            // 已有权限，直接启动
            startVpnService(configJson)
        }
    }

    private fun startVpnService(configJson: String) {
        val intent = Intent(this, MandalaVpnService::class.java).apply {
            action = MandalaVpnService.ACTION_START
            putExtra(MandalaVpnService.EXTRA_CONFIG, configJson)
        }
        startForegroundService(intent) // Android 8.0+ 必须用 startForegroundService
        
        // 通知 ViewModel 更新状态
        // 实际开发中最好由 Service 发广播或 AIDL 通知 Activity，这里简化处理
        val viewModel: MainViewModel = androidx.lifecycle.ViewModelProvider(this)[MainViewModel::class.java]
        viewModel.onVpnStarted()
    }

    private fun stopVpnService() {
        val intent = Intent(this, MandalaVpnService::class.java).apply {
            action = MandalaVpnService.ACTION_STOP
        }
        startService(intent)
    }
}

// MainApp 和 Scaffold 代码保持不变，省略以节省空间...
// 请保留原有的 @Composable MainApp 代码
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainApp(viewModel: MainViewModel) {
    // ... 复制原 MainApp 代码 ...
    // 为了代码完整性，这里我简单复述一下结构
    val navController = rememberNavController()
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
                                popUpTo(navController.graph.startDestinationId) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        }
                    )
                }
            }
        }
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = "Home",
            modifier = Modifier.padding(innerPadding)
        ) {
            composable("Home") { HomeScreen(viewModel) }
            composable("Profiles") { ProfilesScreen(viewModel) }
            composable("Settings") { SettingsScreen(viewModel) }
        }
    }
}
