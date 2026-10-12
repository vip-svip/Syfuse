package io.github.vipsvip.syfuse.ui.nav

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.navigation.NavDestination.Companion.hierarchy
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import io.github.vipsvip.syfuse.ui.page.home.HomePage
import io.github.vipsvip.syfuse.ui.page.nodes.NodesPage
import io.github.vipsvip.syfuse.ui.page.rules.RulesPage
import io.github.vipsvip.syfuse.ui.page.settings.SettingsPage

/**
 * 应用主框架：底部四 Tab + 导航宿主（提示词 5.11）。
 *
 * 导航实现说明：四 Tab 用 NavigationBar + NavHost，而不是简单的状态切换。
 * 原因：提示词 5.11 要求次级页面（连接列表、日志、统计、工具集等）从四 Tab 进入，
 * 这需要真正的返回栈支持，状态切换无法表达「从设置进入关于页再返回」这类路径。
 *
 * 四个 Tab 的状态保持策略：切换 Tab 时各自保留滚动位置与内部状态
 * （通过 NavHost 的 saveState/restoreState），这是 Shadowrocket 的行为等价要求。
 */
@Composable
fun SyfuseApp(modifier: Modifier = Modifier) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentDestination = backStackEntry?.destination

    Scaffold(
        modifier = modifier,
        bottomBar = {
            NavigationBar {
                TopLevelDestination.entries.forEach { destination ->
                    // 用 hierarchy 判断而非直接比较 route：
                    // 进入次级页面时，其父 Tab 应保持选中态
                    val selected = currentDestination?.hierarchy?.any {
                        it.route == destination.route
                    } == true

                    NavigationBarItem(
                        selected = selected,
                        onClick = {
                            navController.navigate(destination.route) {
                                // 四 Tab 间切换不堆积返回栈，避免反复切换后返回键要按很多次
                                popUpTo(navController.graph.findStartDestination().id) {
                                    saveState = true
                                }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = {
                            Icon(
                                imageVector = destination.icon,
                                // 提示词 7.4：全部图标控件必须有 contentDescription
                                contentDescription = stringResource(destination.labelRes),
                            )
                        },
                        label = { Text(stringResource(destination.labelRes)) },
                    )
                }
            }
        },
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = TopLevelDestination.HOME.route,
            modifier = Modifier.padding(innerPadding),
        ) {
            composable(TopLevelDestination.HOME.route) { HomePage() }
            composable(TopLevelDestination.NODES.route) { NodesPage() }
            composable(TopLevelDestination.RULES.route) { RulesPage() }
            composable(TopLevelDestination.SETTINGS.route) { SettingsPage() }

            // 次级页面在 M7 / M8 接入，路由常量已在 Routes 中定义，
            // 此处不提前注册未实现的 composable（避免出现可导航但白屏的路径）
        }
    }
}
