package io.github.vipsvip.syfuse.ui.nav

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import io.github.vipsvip.syfuse.ui.screen.ConnectionsEntry
import io.github.vipsvip.syfuse.ui.screen.DebugScreen
import io.github.vipsvip.syfuse.ui.screen.HomeScreen
import io.github.vipsvip.syfuse.ui.screen.InterceptScreen
import io.github.vipsvip.syfuse.ui.screen.LogsEntry
import io.github.vipsvip.syfuse.ui.screen.MonitorScreen
import io.github.vipsvip.syfuse.ui.screen.ProxyScreen
import io.github.vipsvip.syfuse.ui.screen.RulesScreen
import io.github.vipsvip.syfuse.ui.screen.SettingsScreen
import io.github.vipsvip.syfuse.ui.screen.StatsEntry
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
 *
 * **本文件是「路由注册表」的唯一权威。** 历史上三个二级页（连接/统计/日志）
 * 被写在 `app` 模块，而依赖方向是 `app → ui`，`ui` 无法引用它们 —— 页面
 * 编译得过、测试全绿、APK 正常，但**用户永远打不开**。因此现在有
 * `ui/src/test/.../navigation/NavigationReachabilityTest.kt` 做静态守卫：
 * 要么 `composable(...)` 里注册了路由且底部栏/二级入口能到达，要么测试失败。
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
                composable(AppRoutes.HOME) {
                    HomeScreen(onOpen = { route -> navController.navigate(route) })
                }
                composable(AppRoutes.PROXY) { ProxyScreen() }
                composable(AppRoutes.RULES) { RulesScreen() }
                composable(AppRoutes.BLOCK) { InterceptScreen() }
                composable(AppRoutes.DEBUG) { DebugScreen() }
                composable(AppRoutes.MONITOR) {
                    MonitorScreen(onOpen = { route -> navController.navigate(route) })
                }
                composable(AppRoutes.CONSOLE) {
                    MonitorScreen(onOpen = { route -> navController.navigate(route) })
                }
                composable(AppRoutes.SETTINGS) { SettingsScreen() }

                // M9「数据与统计」三页。此前它们写在 app 模块
                // （app/src/main/kotlin/.../ui/{connections,stats,logs}/），而依赖
                // 方向是 app → ui，ui 无法引用 app 的类 —— 于是三个页面全仓无人
                // 引用，编译得过、测试全绿、APK 正常，但用户永远打不开。
                // 现已迁入本模块（ui/.../ui/screen/）并在此真正挂上路由此。
                // 它们不是底部栏 Tab，所以用带返回按钮的 SecondaryPageScaffold 包裹。
                composable(AppRoutes.CONNECTIONS) {
                    SecondaryPageScaffold("连接", onBack = { navController.popBackStack() }) {
                        ConnectionsEntry()
                    }
                }
                composable(AppRoutes.STATS) {
                    SecondaryPageScaffold("统计", onBack = { navController.popBackStack() }) {
                        StatsEntry()
                    }
                }
                composable(AppRoutes.LOGS) {
                    SecondaryPageScaffold("日志", onBack = { navController.popBackStack() }) {
                        LogsEntry()
                    }
                }
            }
        }
    }
}

/**
 * 二级页外壳：给「没有底部栏 Tab 的页面」一个返回入口。
 *
 * 为什么需要：连接/统计/日志三页不是底部栏 Tab（它们是「监控」Tab 下的
 * 二级页）。只挂路由而不给返回按钮，用户点进去会被困住 —— 只能按系统返回键。
 * 系统返回键在多数场景可用，但**屏幕上没有任何返回提示**属于导航缺陷，
 * 所以这里显式给一个。
 *
 * 注意 `Scaffold` 用的是**内层**的：外层 `SyfuseApp` 的 Scaffold 负责底部栏，
 * 这里只负责这一页自己的顶栏，两者嵌套是 Compose 导航的常规做法。
 *
 * `TopAppBar` 在 Material3 里仍是 `@ExperimentalMaterial3Api`，所以这里显式
 * `@OptIn` 而不是加编译器全局开关 —— 只有这一个函数用了实验 API，把 opt-in
 * 收在最小范围内，将来它转正时删一行即可。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SecondaryPageScaffold(
    title: String,
    onBack: () -> Unit,
    content: @Composable () -> Unit,
) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(title) },
                navigationIcon = {
                    IconButton(
                        onClick = onBack,
                        modifier = Modifier.semantics { contentDescription = "返回" },
                    ) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
            )
        },
    ) { innerPadding ->
        Box(Modifier.padding(innerPadding)) {
            content()
        }
    }
}
