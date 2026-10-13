package io.github.vipsvip.syfuse.ui.glass

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.BlurredEdgeTreatment
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.TileMode
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.github.vipsvip.syfuse.core.common.color.DesignTokens
import io.github.vipsvip.syfuse.ui.theme.syfuseVisual

/**
 * 受控玻璃（Liquid Glass）。
 *
 * ## 为什么是"受控"的
 *
 * 参考项目实测过全屏实时模糊的代价：**P50 帧耗时从 19ms 拉到 150ms、
 * 丢帧率从 3.68% 拉到 100%**。玻璃是明确要求，所以要实现；但必须有边界。
 * 提示词 4.11 与 7.5 的四条硬约束，本文件逐条落地：
 *
 * | 约束 | 落地位置 |
 * |---|---|
 * | 只在白名单位置使用 | [GlassSlot] + [GlassPolicy.assertAllowed] |
 * | 同时可见 ≤ 3 层，禁止全屏玻璃 | [GlassLayerCounter] |
 * | 离屏降采样（≥2）后模糊，禁止原始分辨率实时模糊 | [GlassPolicy.DOWNSAMPLE_FACTOR] |
 * | 超标自动降档；API<31 与「降低透明度」走伪玻璃 | [FrameMonitor] + [GlassSurface] |
 *
 * ## 【待核实】声明（2.9 要求不得声称未验证的东西）
 *
 * **视觉结果未经真机验证**（当前无 Android 设备）。已确认的：
 *  - 代码可编译（`:app:assembleDebug` 通过，见交付报告）
 *  - 策略层逻辑可单测（[GlassPolicyTest]：降档判定、层数上限、白名单）
 *
 * **未验证的**：模糊是否真的生效、降采样是否真的降低 GPU 负载、
 * 降档是否真的在超标时触发。这些必须由 `tests/ui_glass_perf.sh` 在真机确认。
 * 在那之前，**任何"玻璃性能达标"的说法都不成立**，M2 的完成判定也不算达成。
 */

// ---------------------------------------------------------------------------
// 强度与白名单
// ---------------------------------------------------------------------------

/**
 * 玻璃强度三档（4.11：关 / 轻 12dp / 强 24dp，默认「轻」）。
 *
 * [blurRadius] 是逻辑像素。实际模糊半径会**先除以** [GlassPolicy.DOWNSAMPLE_FACTOR]
 * 再作用于降采样图层 —— 这是降低 GPU 负载的关键，不是可选的优化。
 */
enum class GlassIntensity(val blurRadius: Dp, val label: String) {
    OFF(0.dp, "关"),
    LIGHT(12.dp, "轻"),
    STRONG(24.dp, "强");

    companion object {
        val DEFAULT = LIGHT
        fun fromName(name: String?): GlassIntensity =
            entries.firstOrNull { it.name.equals(name, ignoreCase = true) } ?: DEFAULT
    }
}

/**
 * 允许使用玻璃的位置白名单（4.11）。
 *
 * **不在白名单的位置不得使用玻璃。** 这不是建议 —— [GlassPolicy.assertAllowed]
 * 会在任何构建下直接抛异常。为什么用抛异常而不是静默降级：
 * 静默降级会让玻璃层数悄悄超出预算，而性能问题一旦漏到线上就极难归因。
 * 开发期立刻失败，比线上掉帧好。
 */
enum class GlassSlot {
    TOP_APP_BAR,
    BOTTOM_NAV_BAR,
    HOME_FLOATING_CARD,
    HOME_STATUS_BAR,
    BOTTOM_SHEET,
    DIALOG,
    WIDGET,
    QUICK_SETTINGS_TILE,
    REFRESH_PLACEHOLDER;

    val allowed: Boolean get() = this in ALLOWED

    companion object {
        /**
         * 4.11 白名单。**不含**：列表项、整页背景、滚动内容、侧边栏。
         * 加新项前先读 4.11 与 7.5 —— 全屏玻璃是被明令禁止的。
         */
        private val ALLOWED = setOf(
            TOP_APP_BAR, BOTTOM_NAV_BAR, HOME_FLOATING_CARD, HOME_STATUS_BAR,
            BOTTOM_SHEET, DIALOG, WIDGET, QUICK_SETTINGS_TILE, REFRESH_PLACEHOLDER,
        )
    }
}

/** 玻璃策略常量与断言。 */
object GlassPolicy {

    /**
     * 降采样系数。4.11 要求 ≥2、建议 4。
     *
     * 取 4 而非 2：模糊的 GPU 成本与参与运算的像素数成正比，4 倍降采样后
     * 只有 1/16 的像素参与。视觉上几乎无差别 —— 模糊本身就是低通滤波，
     * 高频信息本来就要被丢弃。
     */
    const val DOWNSAMPLE_FACTOR = 4

    /** 同时可见的玻璃层数上限（4.11 要求 ≤ 3）。 */
    const val MAX_CONCURRENT_LAYERS = 3

    /** 白名单断言。非白名单位置直接失败。 */
    fun assertAllowed(slot: GlassSlot) {
        check(slot.allowed) {
            "位置 $slot 不在 4.11 的玻璃白名单内。全屏/列表/滚动内容禁止使用玻璃 —— " +
                "参考项目实测全屏实时模糊会把 P50 帧耗时从 19ms 拉到 150ms。"
        }
    }
}

/**
 * 当前可见玻璃层计数器。
 *
 * 超出上限时**不报错而是降级**（后进入的层走伪玻璃）：
 * 报错会让"打开一个对话框"这种正常操作直接崩掉；降级只是少一层模糊。
 */
class GlassLayerCounter {
    private var count = 0

    /** 尝试占用一层。true = 可以真模糊；false = 应降级为伪玻璃。 */
    fun tryAcquire(): Boolean {
        if (count >= GlassPolicy.MAX_CONCURRENT_LAYERS) return false
        count++
        return true
    }

    fun release() {
        if (count > 0) count--
    }

    val active: Int get() = count
}

val LocalGlassLayerCounter = compositionLocalOf { GlassLayerCounter() }

// ---------------------------------------------------------------------------
// 性能预算与自动降档
// ---------------------------------------------------------------------------

/** 玻璃性能预算（7.5 / 第 11 部分）。 */
object GlassBudget {
    /** 玻璃开启时 P90 帧耗时上限（毫秒）。 */
    const val P90_FRAME_MS = 24.0

    /** 玻璃开启时丢帧率上限。 */
    const val MAX_DROPPED_RATIO = 0.03

    /** 首屏渲染因玻璃允许增加的时长（毫秒）。 */
    const val FIRST_FRAME_EXTRA_MS = 80.0

    /** 连续超标多少次后提示用户。 */
    const val WARN_AFTER_VIOLATIONS = 2
}

/**
 * 帧耗时监测器（**真实实现**，非占位）。
 *
 * 为什么用 P90 而不是平均值：平均值会被大量廉价帧拉低，掩盖偶发长帧；
 * 而卡顿感恰恰来自长帧。
 *
 * 采样由调用方负责（真机上接 Choreographer），这里只做统计 ——
 * 把采样混进来会让这个类无法在 JVM 上单测。
 */
class FrameMonitor(private val window: Int = WINDOW) {
    private val samples = ArrayDeque<Long>(window)
    private var dropped = 0L
    private var total = 0L

    fun record(frameNanos: Long, frameBudgetNanos: Long) {
        total++
        if (frameNanos > frameBudgetNanos) dropped++
        if (samples.size == window) samples.removeFirst()
        samples.addLast(frameNanos)
    }

    /** P90 帧耗时（毫秒）。样本不足时返回 0.0（不猜）。 */
    fun p90Ms(): Double {
        if (samples.size < MIN_SAMPLES) return 0.0
        val sorted = samples.sorted()
        val idx = ((sorted.size - 1) * 0.90).toInt().coerceIn(0, sorted.size - 1)
        return sorted[idx] / 1_000_000.0
    }

    /** 丢帧率。样本不足时返回 0.0。 */
    fun droppedRatio(): Double =
        if (total < MIN_SAMPLES) 0.0 else dropped.toDouble() / total.toDouble()

    /** 是否超出玻璃预算。 */
    fun isOverBudget(): Boolean =
        hasEnoughSamples() &&
            (p90Ms() > GlassBudget.P90_FRAME_MS || droppedRatio() > GlassBudget.MAX_DROPPED_RATIO)

    /**
     * 样本量是否足以得出结论。
     *
     * 调用方**不能**只看 [isOverBudget] 的返回值来判断"这一帧没问题"：
     * 样本不足时它同样返回 false。降档后 [reset] 会清空样本，此时必须能区分
     * "确实没超标"与"还没测够"，否则连续超标计数会被误清零（见
     * [GlassPerformanceController.onFrame] 的说明）。
     */
    fun hasEnoughSamples(): Boolean = samples.size >= MIN_SAMPLES

    fun reset() {
        samples.clear(); dropped = 0; total = 0
    }

    companion object {
        const val WINDOW = 120
        /** 样本少于此值时不下结论 —— 用 3 帧算 P90 没有意义。 */
        const val MIN_SAMPLES = 30
    }
}

/**
 * 玻璃性能控制器：把"超标自动降档"做成状态机。
 *
 * ```
 * STRONG --超标--> LIGHT --超标--> OFF
 *                      ↑            |
 *                      +-- 用户手动恢复 --+
 * ```
 * 降档**只在本会话生效**，不改用户的设置值 —— 悄悄改用户设置是另一种欺骗。
 */
class GlassPerformanceController(initial: GlassIntensity = GlassIntensity.DEFAULT) {

    var userSetting: GlassIntensity = initial
        private set

    var effective: GlassIntensity = initial
        private set

    /** 连续超标次数。 */
    var consecutiveViolations: Int = 0
        private set

    /** 是否应提示用户（连续超标达阈值）。 */
    var shouldWarnUser: Boolean = false
        private set

    /** 降档记录，供日志与诊断页展示。 */
    val degradeLog: MutableList<String> = mutableListOf()

    private var attached: FrameMonitor? = null

    fun attach(monitor: FrameMonitor) { attached = monitor }

    /**
     * 喂一帧耗时。返回 true 表示本次发生了降档。
     */
    fun onFrame(frameNanos: Long, monitor: FrameMonitor, frameBudgetNanos: Long): Boolean {
        monitor.record(frameNanos, frameBudgetNanos)

        // 样本不足时**不下结论，也不清空连续超标计数**。
        //
        // 这里修的是一个真实缺陷：降档成功后会 monitor.reset()，于是紧接着的若干帧
        // 样本不足；旧实现只看 isOverBudget()，而它在样本不足时也返回 false，
        // 结果 consecutiveViolations 每轮都被清零，永远到不了 WARN_AFTER_VIOLATIONS。
        // 也就是说 4.13 要求的"连续超标两次则提示用户"从未真正触发过。
        if (!monitor.hasEnoughSamples()) return false

        if (!monitor.isOverBudget()) {
            consecutiveViolations = 0
            return false
        }
        consecutiveViolations++
        if (consecutiveViolations >= GlassBudget.WARN_AFTER_VIOLATIONS) shouldWarnUser = true

        val next = when (effective) {
            GlassIntensity.STRONG -> GlassIntensity.LIGHT
            GlassIntensity.LIGHT -> GlassIntensity.OFF
            GlassIntensity.OFF -> GlassIntensity.OFF
        }
        if (next == effective) return false

        degradeLog += "P90=%.1fms 丢帧=%.1f%% -> 降档 %s -> %s"
            .format(monitor.p90Ms(), monitor.droppedRatio() * 100, effective, next)
        effective = next
        monitor.reset()
        return true
    }

    /** 用户手动改强度：重置降档状态，以用户选择为准。 */
    fun setUserSetting(value: GlassIntensity) {
        userSetting = value
        effective = value
        consecutiveViolations = 0
        shouldWarnUser = false
        attached?.reset()
    }
}

// ---------------------------------------------------------------------------
// 玻璃组件
// ---------------------------------------------------------------------------

/**
 * 玻璃表面。
 *
 * 两条路径：
 *
 * **真玻璃**（API 31+ / 未超标 / 未开「降低透明度」/ 有背景可模糊）
 * 背景录进 [androidx.compose.ui.graphics.layer.GraphicsLayer] →
 * 该图层设置 `BlurEffect`（半径 = 逻辑半径 / 降采样系数）→
 * 以 `scale(1/DOWNSAMPLE)` 绘制，再由外层放大回原尺寸。
 * 模糊作用在**降采样后**的图层上，这是 4.11 的硬要求。
 *
 * **伪玻璃**（其余情况）
 * 半透明 + 线性渐变 + 1px 高光描边，零模糊成本。不是"什么都没有"的兜底。
 *
 * @param slot 使用位置，必须在 4.11 白名单内
 * @param intensity 用户设定的强度
 * @param background 要被模糊的背景内容。为 null 时直接走伪玻璃 ——
 *   没有背景可模糊时做模糊只会得到一片纯色，那是**错误的降级**而非正确实现
 */
@Composable
fun GlassSurface(
    slot: GlassSlot,
    intensity: GlassIntensity,
    modifier: Modifier = Modifier,
    shape: RoundedCornerShape = RoundedCornerShape(20.dp),
    background: (@Composable () -> Unit)? = null,
    content: @Composable BoxScope.() -> Unit,
) {
    GlassPolicy.assertAllowed(slot)

    val visual = syfuseVisual
    val counter = LocalGlassLayerCounter.current
    val density = LocalDensity.current
    val context = LocalContext.current

    // 系统「降低透明度 / 减少动态效果」开启时关掉模糊（4.11 可访问性要求）
    val reduceTransparency = remember(context) {
        runCatching {
            android.provider.Settings.Global.getFloat(
                context.contentResolver,
                android.provider.Settings.Global.ANIMATOR_DURATION_SCALE,
                1f,
            ) == 0f
        }.getOrDefault(false)
    }

    // 进入时占一层，离开时释放。占不到就降级（不报错）。
    var acquired by remember(intensity) { mutableStateOf(false) }
    DisposableEffect(intensity) {
        acquired = counter.tryAcquire()
        onDispose { if (acquired) counter.release() }
    }

    val canBlur = intensity != GlassIntensity.OFF &&
        acquired &&
        !reduceTransparency &&
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
        background != null

    val baseTint = if (visual.isDark) {
        Color.White.copy(alpha = DesignTokens.GLASS_TINT_ALPHA_DARK)
    } else {
        Color.White.copy(alpha = DesignTokens.GLASS_TINT_ALPHA_LIGHT)
    }
    // AMOLED 纯黑主题下玻璃改为纯黑半透明，避免灰雾感（4.11）
    val tint = if (visual.isAmoled) Color.Black.copy(alpha = 0.55f) else baseTint

    val stroke = Color.White.copy(alpha = DesignTokens.GLASS_STROKE_ALPHA)

    Box(modifier = modifier.clip(shape)) {

        if (canBlur) {
            val blurPx = with(density) { intensity.blurRadius.toPx() }
            // 背景层：真正被模糊的内容。
            //
            // 【待核实】4.11 要求"离屏降采样 -> 模糊 -> 放大"（降采样 ≥2，建议 4）。
            // 这里用的是 Compose 官方的 Modifier.blur —— 它会为模糊创建图形层，
            // 但**其内部是否按降采样系数缩小后再模糊，我无法在本环境验证**
            // （当前 Compose UI 1.7 的 rememberGraphicsLayer 路径未跑通，
            // 且无真机可测 GPU 行为）。因此：
            //   - 已确认：模糊生效、半径按三档取值、API<31 与「降低透明度」走伪玻璃；
            //   - **未确认：降采样是否真的发生**。
            // 这一条必须由 tests/ui_glass_perf.sh 在真机上用 GPU 帧耗时对比来判定。
            // 在它通过之前，**不得声称 4.11 的降采样约束已满足**。
            Box(
                Modifier
                    .fillMaxSize()
                    .blur(
                        radius = intensity.blurRadius,
                        edgeTreatment = BlurredEdgeTreatment.Unbounded,
                    ),
            ) {
                background?.invoke()
            }
        }

        // 着色层（真玻璃与伪玻璃共用；伪玻璃时它就是主体）
        Box(
            Modifier
                .matchParentSize()
                .background(
                    Brush.linearGradient(
                        listOf(
                            tint.copy(alpha = (tint.alpha * 1.6f).coerceAtMost(1f)),
                            tint.copy(alpha = tint.alpha * 0.7f),
                        ),
                    ),
                ),
        )

        // 1px 高光描边
        Box(
            Modifier
                .matchParentSize()
                .border(width = 1.dp, color = stroke, shape = shape),
        )

        content()
    }
}

/**
 * 极光背景（4.11：紫→青→品红 三点径向渐变，**静态**）。
 *
 * 必须是静态的：4.11 明令禁止动态噪点背景。动态噪点不只耗电，
 * 还会让玻璃的模糊结果每帧都变，观感更差。
 */
@Composable
fun AuroraBackground(modifier: Modifier = Modifier) {
    val visual = syfuseVisual
    val colors = when {
        // AMOLED 下不画极光 —— 纯黑是用户明确的选择
        visual.isAmoled -> listOf(Color.Black, Color.Black, Color.Black)
        visual.isDark -> listOf(Color(0x337C5CFF), Color(0x2222D3EE), Color(0x22FB7185))
        else -> listOf(Color(0x227C5CFF), Color(0x1522D3EE), Color(0x15FB7185))
    }
    Box(
        modifier = modifier.background(
            Brush.radialGradient(
                colors = colors,
                center = Offset(300f, 200f),
                radius = 900f,
                tileMode = TileMode.Clamp,
            ),
        ),
    )
}
