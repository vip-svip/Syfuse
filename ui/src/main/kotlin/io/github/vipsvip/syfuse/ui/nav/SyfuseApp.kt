package io.github.vipsvip.syfuse.ui.nav

import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.SwapVert
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.navigation.NavDestination.Companion.hierarchy
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import io.github.vipsvip.syfuse.data.settings.AppSettingsDataStore
import io.github.vipsvip.syfuse.domain.settings.SettingsCatalog
import io.github.vipsvip.syfuse.domain.settings.ThemeMode
import io.github.vipsvip.syfuse.ui.screen.HomeScreen
import io.github.vipsvip.syfuse.ui.screen.NodesScreen
import io.github.vipsvip.syfuse.ui.screen.RulesScreen
import io.github.vipsvip.syfuse.ui.screen.SettingsScreen
import io.github.vipsvip.syfuse.ui.theme.SyfuseTheme

/** 底部四个主 Tab，对齐 Shadowrocket 的四段式（5.11）。 */
enum class SyfuseTab(val route: String, val label: String, val icon: ImageVector) {
    HOME("home", "首页", Icons.Filled.Home),
    NODES("nodes", "节点", Icons.Filled.SwapVert),
    RULES("rules", "规则", Icons.Filled.Shield),
    SETTINGS("settings", "设置", Icons.Filled.Settings),
}

/**
 * 应用根组件。
 *
 * 主题与语言由设置驱动。注意：主界面完全由 Compose 构建，**没有任何 WebView 节点**——
 * 这是 R1 的判定方式（检索 WebView / loadUrl，主 Activity 树无 WebView 节点）。
 */
@Composable
fun SyfuseApp(
    settingsStore: AppSettingsDataStore,
) {
    val settings by settingsStore.settings.collectAsState(initial = null)

    val themeMode = settings?.get(SettingsCatalog.byKey("theme")?.key ?: "theme")
        ?.let { runCatching { ThemeMode.valueOf(it.uppercase()) }.getOrNull() }
        ?: ThemeMode.SYSTEM
    val dynamicColor = settings?.getBoolean("dynamic_color") ?: true

    SyfuseTheme(themeMode = themeMode, dynamicColor = dynamicColor) {
        val navController = rememberNavController()
        val backStackEntry by navController.currentBackStackEntryAsState()
        val currentDestination = backStackEntry?.destination

        Scaffold(
            bottomBar = {
                NavigationBar {
                    SyfuseTab.entries.forEach { tab ->
                        val selected = currentDestination?.hierarchy?.any { it.route == tab.route } == true
                        NavigationBarItem(
                            selected = selected,
                            onClick = {
                                navController.navigate(tab.route) {
                                    popUpTo(navController.graph.findStartDestination().id) {
                                        saveState = true
                                    }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            },
                            icon = {
                                Icon(
                                    tab.icon,
                                    contentDescription = tab.label,
                                    modifier = Modifier.semantics { contentDescription = tab.label },
                                )
                            },
                            label = { Text(tab.label) },
                        )
                    }
                }
            },
        ) { innerPadding ->
            NavHost(
                navController = navController,
                startDestination = SyfuseTab.HOME.route,
                modifier = Modifier.padding(innerPadding),
            ) {
                composable(SyfuseTab.HOME.route) { HomeScreen() }
                composable(SyfuseTab.NODES.route) { NodesScreen() }
                composable(SyfuseTab.RULES.route) { RulesScreen() }
                composable(SyfuseTab.SETTINGS.route) { SettingsScreen() }
            }
        }
    }
}
