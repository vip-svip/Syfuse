package io.github.vipsvip.syfuse.ui.nav

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 导航可达性守卫。
 *
 * ## 为什么需要这个测试
 *
 * 本仓库真实发生过一次严重缺陷：三个二级页（连接 / 统计 / 日志）写在 `app`
 * 模块，而导航树在 `ui` 模块，依赖方向是 `app → ui` —— `ui` **无法** import
 * `app` 的类。结果是这三个页面：
 *   - 编译通过；
 *   - 单元测试全绿（290 个测试 0 失败）；
 *   - `:app:assembleDebug` 成功；
 *   - **用户在任何一个界面上都点不到它们**。
 *
 * 全仓 grep 这三个页面名，只有它们自身的定义匹配 —— 零外部引用。
 * 而当时负责"引流"的 `MonitorScreen` 反而对这些*已实现*的能力显示红字
 * 「未实现」，把真实状态反过来汇报。
 *
 * 这套测试用**静态源码分析**把这类问题变成编译期之后、提测之前的硬失败：
 * 它不依赖 Android 运行时，因此能在普通 JVM 单测里跑。
 *
 * ## 它检查什么
 *
 * 1. `SyfuseApp.kt` 里注册的每个 `composable("x")` 都在 [AppRoutes.ALL] 登记；
 * 2. [AppRoutes.ALL] 里的每个路由都在 `SyfuseApp.kt` 里真的注册了；
 * 3. 每个二级页路由都有入口点 —— 即"注册了但没人点得到"也算失败；
 * 4. `MonitorScreen` 的能力表与 [AppRoutes] 一致：凡是标注"可用"的，
 *    路由必须真实存在（防止再次出现"界面上可点但路由不存在"的崩溃，
 *    或"路由存在但界面说未实现"的错误汇报）。
 *
 * ## 已知边界（不粉饰）
 *
 * 这是**静态文本分析**，不是 Compose UI 测试。它能抓住"路由漏注册"和
 * "二级页无入口"，但抓不住"点击后崩溃"「渲染错位」这类运行时问题 ——
 * 那需要 `androidTest` + 真机，本模块没有该源集。见 [doesNotVerifyRuntimeBehaviour]。
 */
class NavigationReachabilityTest {

    private val uiModuleRoot: File = findUiModuleRoot()
    private val syfuseAppSource: String =
        File(uiModuleRoot, "src/main/kotlin/io/github/vipsvip/syfuse/ui/nav/SyfuseApp.kt").readText()
    private val monitorSource: String =
        File(uiModuleRoot, "src/main/kotlin/io/github/vipsvip/syfuse/ui/screen/MonitorScreen.kt").readText()

    /** 定位 ui 模块根目录。单测的工作目录是模块根，但不同 Gradle 版本可能不同，故向上找。 */
    private fun findUiModuleRoot(): File {
        var dir: File? = File(".").absoluteFile
        while (dir != null) {
            val candidate = File(dir, "src/main/kotlin/io/github/vipsvip/syfuse/ui/nav")
            if (candidate.isDirectory) return dir
            dir = dir.parentFile
        }
        error(
            "找不到 ui 模块根目录（期望存在 src/main/kotlin/io/github/vipsvip/syfuse/ui/nav）。" +
                "当前工作目录：${File(".").absolutePath}",
        )
    }

    /** 从 `SyfuseApp.kt` 里抽出所有 `composable(...)` 的路由字面量。 */
    private fun registeredRoutes(source: String): Set<String> {
        // 兼容 composable(AppRoutes.HOME) 与 composable("home") 两种写法。
        val direct = Regex("""composable\(\s*"([^"]+)"\s*\)""").findAll(source).map { it.groupValues[1] }
        val viaConstant = Regex("""composable\(\s*AppRoutes\.([A-Z_]+)\s*\)""")
            .findAll(source)
            .map { it.groupValues[1] }
        val constants = viaConstant.map { name ->
            // 把 AppRoutes.XXX 解析成它的字符串值，用反射读 const val。
            val field = AppRoutes::class.java.getDeclaredField(name)
            field.isAccessible = true
            field.get(null) as String
        }
        return (direct + constants).toSet()
    }

    @Test
    fun `SyfuseApp 里注册的每个路由都在 AppRoutes 里登记`() {
        val registered = registeredRoutes(syfuseAppSource)
        val unregistered = registered - AppRoutes.ALL
        assertThat(unregistered).isEmpty()
    }

    @Test
    fun `AppRoutes 里的每个路由都在 SyfuseApp 里真实注册`() {
        val registered = registeredRoutes(syfuseAppSource)
        val missing = AppRoutes.ALL - registered
        assertThat(missing).isEmpty()
    }

    /**
     * 关键用例：二级页必须有入口点。
     *
     * 这条直接对应"页面写了但没有入口"的历史缺陷 —— 如果哪天有人把
     * `MonitorScreen` 里的入口删掉（或把连接页从监控 Tab 移走却没接新入口），
     * 这个测试会失败，而不是等用户发现点不到。
     */
    @Test
    fun `每个二级页路由都有可达入口`() {
        val secondary = AppRoutes.ALL - setOf(
            AppRoutes.HOME, AppRoutes.PROXY, AppRoutes.RULES, AppRoutes.BLOCK,
            AppRoutes.DEBUG, AppRoutes.MONITOR, AppRoutes.CONSOLE, AppRoutes.SETTINGS,
        )
        assertThat(secondary).isNotEmpty()

        for (route in secondary) {
            val entries = AppRoutes.ENTRY_POINTS[route]
            assertThat(entries).isNotNull()
            assertThat(entries).isNotEmpty()
            // 入口本身必须是一级 Tab（否则形成"要到达 A 得先到达 B，而 B 也没入口"的断链）。
            for (entry in entries!!) {
                assertThat(AppRoutes.ALL).contains(entry)
            }
        }
    }

    /**
     * `MonitorScreen` 声明"可用"的条目，其路由必须真实注册。
     *
     * 防的是：界面显示可点（有右箭头、`isReady == true`），但 `SyfuseApp.kt`
     * 里根本没有这个路由 —— 用户一点就崩（NavController 抛
     * `IllegalArgumentException: Navigation destination ... not found`）。
     */
    @Test
    fun `MonitorScreen 里可点的条目其路由都真实注册`() {
        val registered = registeredRoutes(syfuseAppSource)
        // 抽出 monitorEntries 里所有 route = "xxx" 字面量。
        val readyRoutes = Regex("""route\s*=\s*"([^"]+)"""")
            .findAll(monitorSource)
            .map { it.groupValues[1] }
            .toSet()

        assertThat(readyRoutes).isNotEmpty()
        for (route in readyRoutes) {
            assertThat(registered).contains(route)
        }
    }

    /**
     * `MonitorScreen` 的能力表必须与 `AppRoutes` 同步。
     *
     * 反向守卫：三个已实现的二级页（连接/统计/日志）必须出现在 [AppRoutes] 的
     * 二级页集合里，且 `MonitorScreen` 必须给它们配了入口 —— 否则用户又会
     * 看到"功能已实现但界面说未实现"。
     */
    @Test
    fun `MonitorScreen 为每个二级页都提供了入口`() {
        val readyRoutes = Regex("""route\s*=\s*"([^"]+)"""")
            .findAll(monitorSource)
            .map { it.groupValues[1] }
            .toSet()
        val secondary = AppRoutes.ENTRY_POINTS.keys
        val missing = secondary - readyRoutes
        assertThat(missing).isEmpty()
    }

    /**
     * 每个二级页路由都要有返回出口 —— 页面不能是"只能进不能出"的死胡同。
     *
     * 检查 `SyfuseApp.kt` 里二级页是否被 [SecondaryPageScaffold]（带返回按钮）
     * 包裹，而不是裸调 `XxxEntry()`。
     */
    @Test
    fun `二级页都带返回按钮而不是裸页面`() {
        val secondaryBlocks = Regex(
            """composable\(AppRoutes\.(CONNECTIONS|STATS|LOGS)\)\s*\{([^}]*SecondaryPageScaffold[^}]*)\}""",
            RegexOption.DOT_MATCHES_ALL,
        ).findAll(syfuseAppSource).count()

        assertThat(secondaryBlocks).isEqualTo(3)
    }

    /**
     * 明确记录本测试**不能**覆盖什么，避免读者误以为它等价于 UI 测试。
     * 这条断言本身永远为真，它的价值是文档 —— 用一个测试名把边界写进报告。
     */
    @Test
    fun `doesNotVerifyRuntimeBehaviour`() {
        // 本模块没有 androidTest 源集，也没有 Compose UI 测试依赖（已实测
        // ui/build.gradle.kts：无 androidTestImplementation / ui-test-junit4）。
        // 因此"点击是否崩溃""布局是否错位""真机上能否渲染"都**未**被覆盖。
        // 要真正闭环需要真机 + VpnService，见 PENDING-EXTERNAL.md。
        assertThat(uiModuleRoot.isDirectory).isTrue()
    }
}
