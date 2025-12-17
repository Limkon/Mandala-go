// 文件路径: android/app/src/main/java/com/example/mandala/MainActivity.kt

package com.example.mandala

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.example.mandala.ui.home.HomeScreen
import com.example.mandala.ui.profiles.ProfilesScreen
import com.example.mandala.ui.settings.SettingsScreen
import com.example.mandala.viewmodel.MainViewModel

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            // 使用 MaterialTheme
            MaterialTheme {
                MainApp()
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainApp() {
    val navController = rememberNavController()
    // 获取 ViewModel 实例
    val viewModel: MainViewModel = viewModel()
    
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
            startDestination = "Home", // 路由 Key 保持英文，方便内部逻辑引用
            modifier = Modifier.padding(innerPadding)
        ) {
            composable("Home") { HomeScreen(viewModel) }
            composable("Profiles") { ProfilesScreen(viewModel) }
            composable("Settings") { SettingsScreen(viewModel) }
        }
    }
}
