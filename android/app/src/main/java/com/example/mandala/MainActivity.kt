// 文件路徑: android/app/src/main/java/com/example/mandala/MainActivity.kt

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
import com.example.mandala.ui.theme.MandalaTheme // 需確保已有 Theme 文件，或使用 MaterialTheme 代替
import com.example.mandala.viewmodel.MainViewModel

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            // 使用默認 MaterialTheme，如果你有自定義 Theme 請替換
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
    // 獲取 ViewModel 實例
    val viewModel: MainViewModel = viewModel()
    
    // 底部導航配置
    val items = listOf("Home", "Profiles", "Settings")
    val icons = listOf(
        Icons.Filled.Home,
        Icons.Filled.List,
        Icons.Filled.Settings
    )

    Scaffold(
        bottomBar = {
            NavigationBar {
                val navBackStackEntry by navController.currentBackStackEntryAsState()
                val currentRoute = navBackStackEntry?.destination?.route

                items.forEachIndexed { index, screen ->
                    NavigationBarItem(
                        icon = { Icon(icons[index], contentDescription = screen) },
                        label = { Text(screen) },
                        selected = currentRoute == screen,
                        onClick = {
                            navController.navigate(screen) {
                                // 避免堆疊過多頁面
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
        // 導航主機
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
