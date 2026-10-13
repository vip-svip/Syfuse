package io.github.vipsvip.syfuse.ui.nav

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
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
import io.github.vipsvip.syfuse.ui.screen.DebugScreen
import io.github.vipsvip.syfuse.ui.screen.HomeScreen
import io.github.vipsvip.syfuse.ui.screen.InterceptScreen
import io.github.vipsvip.syfuse.ui.screen.MonitorScreen
import io.github.vipsvip.syfuse.ui.screen.ProxyScreen
import io.github.vipsvip.syfuse.ui.screen.RulesScreen
import io.github.vipsvip.syfuse.ui.screen.SettingsScreen
import io.github.vipsvip.syfuse.ui.theme.SyfuseTheme

/**
 * 应用根组件。
 *
 * **导航由布局预设驱动**（提示词 4.1）。默认布局 B「极客全显」7 Tab：
 * 首页 / 代理 / 规则 / 拦截 / 调试 / 监控 / 设置。
 *
 * 为什么不是固定的 4 Tab：4.1 复盘指出，把「功能域」直接当 Tab 会导致
 * 单 Tab 超载（原「配置」要装分流+过滤+模块+脚本四种东西）。
 * 正确做法是按用户任务切分，**且布局本身可换**——所以 Tab 列表来自
 * [LayoutPresets] 而不是写死在这里。
 *
 * 主界面完全由 Compose 构建，**没有任何 WebView 节点** —— 这是 R1 的判定方式。
 */
@Composable
fun SyfuseApp(
    settingsStore: AppSettingsDataStore,
    layoutId: String? = null,
) {
    val settings by settingsStore.settings.collectAsState(initial = null)

    val themeMode = settings?.get(SettingsCatalog.byKey("theme")?.key ?: "theme")
        ?.let { runCatching { ThemeMode.valueOf(it.uppercase()) }.getOrNull() }
        ?: ThemeMode.SYSTEM
    val dynamicColor = settings?.getBoolean("dynamic_color") ?: false

    // 布局由设置驱动；未设置时用 4.1 规定的默认（布局 B，7 Tab）
    val layout = LayoutPresets.byId(
        layoutId ?: settings?.get("layout_preset"),
    )

    SyfuseTheme(themeMode = themeMode, dynamicColor = dynamicColor) {
        val navController = rememberNavController()
        val backStackEntry by navController.currentBackStackEntryAsState()
        val currentDestination = backStackEntry?.destination

        Scaffold(
            bottomBar = {
                NavigationBar {
                    layout.tabs.forEach { tab ->
                        val selected = currentDestination?.hierarchy?.any { it.route == tab.id } == true
                        NavigationBarItem(
                            selected = selected,
                            onClick = {
                                navController.navigate(tab.id) {
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
                startDestination = layout.tabs.first().id,
                modifier = Modifier.padding(innerPadding),
            ) {
                composable("home") { HomeScreen() }
                composable("proxy") { ProxyScreen() }
                composable("rules") { RulesScreen() }
                composable("block") { InterceptScreen() }
                composable("debug") { DebugScreen() }
                composable("monitor") { MonitorScreen() }
                composable("console") { MonitorScreen() }
                composable("settings") { SettingsScreen() }
            }
        }
    }
}
