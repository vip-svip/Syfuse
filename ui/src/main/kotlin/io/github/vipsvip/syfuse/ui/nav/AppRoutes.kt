package io.github.vipsvip.syfuse.ui.nav

/**
 * 应用的全部导航路由 —— **单一事实来源**。
 *
 * 为什么要有这个对象：本仓库出过一个"页面写了但用户永远打不开"的严重缺陷。
 * 三个二级页（连接 / 统计 / 日志）写在 `app` 模块，而导航树在 `ui` 模块，
 * 依赖方向是 `app → ui`，`ui` 无法 import `app` 的类 —— 于是它们编译得过、
 * 测试全绿、APK 正常，却是**死代码**。而 `MonitorScreen` 当时还对这些
 * *已实现*的能力显示红字「未实现」，把真实状态反过来汇报给用户。
 *
 * 单靠"以后注意点"防不住这类问题，所以这里把路由**变成可被测试断言的常量**：
 * 见 `ui/src/test/kotlin/.../nav/NavigationReachabilityTest.kt`，
 * 它静态解析 `SyfuseApp.kt` 并要求：
 *  1. 每个 [ALL] 里的路由都在 `SyfuseApp.kt` 里真的 `composable(...)` 注册了；
 *  2. 每个非一级 Tab 的路由，都能从某个一级 Tab 或某个二级入口到达
 *     （即"注册了但没人能点到"也会失败）；
 *  3. 反向：`SyfuseApp.kt` 里注册的每个路由都在 [ALL] 里登记。
 */
object AppRoutes {

    /** 一级 Tab 路由。必须与 [LayoutPresets] 各套布局的 `TabSpec.id` 对得上。 */
    const val HOME = "home"
    const val PROXY = "proxy"
    const val RULES = "rules"
    const val BLOCK = "block"
    const val DEBUG = "debug"
    const val MONITOR = "monitor"
    const val CONSOLE = "console"
    const val SETTINGS = "settings"

    /** 二级页路由（没有独立的底部栏 Tab，只能从一级 Tab 里的入口进入）。 */
    const val CONNECTIONS = "connections"
    const val STATS = "stats"
    const val LOGS = "logs"

    /** 全部已注册路由。改动 `SyfuseApp.kt` 的 NavHost 必须同步这里。 */
    val ALL: Set<String> = setOf(
        HOME, PROXY, RULES, BLOCK, DEBUG, MONITOR, CONSOLE, SETTINGS,
        CONNECTIONS, STATS, LOGS,
    )

    /**
     * 二级页 → 能到达它的入口列表。
     *
     * 值为"入口所在的路由"，`null` 表示该入口挂在底部栏（一级 Tab 自身可达）。
     * 这张表让"注册了但无入口"变成测试失败，而不是等用户点不到才发现。
     */
    val ENTRY_POINTS: Map<String, Set<String>> = mapOf(
        CONNECTIONS to setOf(MONITOR, CONSOLE),
        STATS to setOf(MONITOR, CONSOLE),
        LOGS to setOf(MONITOR, CONSOLE),
    )
}
