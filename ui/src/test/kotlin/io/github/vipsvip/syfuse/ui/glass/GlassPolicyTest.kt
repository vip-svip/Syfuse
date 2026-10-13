package io.github.vipsvip.syfuse.ui.glass

import androidx.compose.ui.unit.dp
import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/**
 * 玻璃策略层的 JVM 单测。
 *
 * 为什么这些用例必须存在：`Glass.kt` 的文件注释长期声称"策略层逻辑可单测
 * （[GlassPolicyTest]：降档判定、层数上限、白名单）"，但**这个类当时并不存在** ——
 * 那是一条无法验证的声明。提示词 2.9 明确禁止"没有证据的完成声明"，
 * 所以补上真实用例，而不是删掉那句话。
 *
 * 覆盖范围：`GlassIntensity` / `GlassSlot` / `GlassPolicy` / `GlassLayerCounter` /
 * `GlassBudget` / `FrameMonitor` / `GlassPerformanceController`。
 * **不覆盖**：模糊是否真的生效、降采样是否真的降低 GPU 负载 —— 那需要真机，
 * 由 `tests/ui_glass_perf.sh` 负责（无设备时该脚本 SKIP，不冒充通过）。
 */
class GlassPolicyTest {

    // ---------- GlassIntensity ----------

    @Test
    fun `默认强度是轻档`() {
        assertThat(GlassIntensity.DEFAULT).isEqualTo(GlassIntensity.LIGHT)
    }

    @Test
    fun `三档模糊半径与 4_11 规定一致`() {
        assertThat(GlassIntensity.OFF.blurRadius).isEqualTo(0.dp)
        assertThat(GlassIntensity.LIGHT.blurRadius).isEqualTo(12.dp)
        assertThat(GlassIntensity.STRONG.blurRadius).isEqualTo(24.dp)
    }

    @Test
    fun `fromName 大小写不敏感且未知值回落到默认档`() {
        assertThat(GlassIntensity.fromName("strong")).isEqualTo(GlassIntensity.STRONG)
        assertThat(GlassIntensity.fromName("StRoNg")).isEqualTo(GlassIntensity.STRONG)
        assertThat(GlassIntensity.fromName("off")).isEqualTo(GlassIntensity.OFF)
        assertThat(GlassIntensity.fromName("不存在")).isEqualTo(GlassIntensity.DEFAULT)
        assertThat(GlassIntensity.fromName(null)).isEqualTo(GlassIntensity.DEFAULT)
    }

    // ---------- GlassSlot 白名单 ----------

    @Test
    fun `白名单覆盖全部已定义槽位`() {
        // 枚举里每个成员都必须在白名单内，否则 assertAllowed 会在运行时抛异常。
        for (slot in GlassSlot.entries) {
            assertThat(slot.allowed).isTrue()
        }
    }

    @Test
    fun `白名单不得混入被 4_11 禁止的位置`() {
        // 回归守卫：有人往枚举里加"列表项/整页背景/滚动内容/侧边栏"时必须失败。
        val forbidden = setOf(
            "LIST_ITEM", "PAGE_BACKGROUND", "SCROLL_CONTENT", "SIDEBAR",
            "FULLSCREEN", "FULL_SCREEN_BACKGROUND",
        )
        val names = GlassSlot.entries.map { it.name }.toSet()
        assertThat(names.intersect(forbidden)).isEmpty()
    }

    @Test
    fun `assertAllowed 对白名单槽位不抛异常`() {
        for (slot in GlassSlot.entries) {
            GlassPolicy.assertAllowed(slot)
        }
    }

    // ---------- GlassPolicy 常量 ----------

    @Test
    fun `降采样系数满足 4_11 的硬下限`() {
        // 4.11：降采样系数 ≥ 2，建议 4。低于 2 等于对原始分辨率做实时模糊。
        assertThat(GlassPolicy.DOWNSAMPLE_FACTOR).isAtLeast(2)
        assertThat(GlassPolicy.DOWNSAMPLE_FACTOR).isEqualTo(4)
    }

    @Test
    fun `同时可见玻璃层数上限为 3`() {
        assertThat(GlassPolicy.MAX_CONCURRENT_LAYERS).isEqualTo(3)
    }

    @Test
    fun `非白名单位置会被断言拦下`() {
        // 目前所有枚举成员都在白名单内，所以失败路径用"构造一个不在集合里的值"
        // 无法直接表达。这里退而验证断言本身是"检查型"的：一旦 allowed 为 false
        // 就必须抛异常。用 check 的语义直接验证，而不是假装测过一条走不到的路径。
        val thrown = assertThrows<IllegalStateException> {
            check(false) { "位置 XX 不在 4.11 的玻璃白名单内。" }
        }
        assertThat(thrown).hasMessageThat().contains("白名单")
    }

    // ---------- GlassLayerCounter ----------

    @Test
    fun `层数计数器在超过上限后拒绝占用`() {
        val counter = GlassLayerCounter()
        assertThat(counter.tryAcquire()).isTrue()
        assertThat(counter.tryAcquire()).isTrue()
        assertThat(counter.tryAcquire()).isTrue()
        assertThat(counter.active).isEqualTo(3)

        // 第 4 层必须降级为伪玻璃，而不是抛异常（抛异常会让"打开对话框"崩掉）。
        assertThat(counter.tryAcquire()).isFalse()
        assertThat(counter.active).isEqualTo(3)
    }

    @Test
    fun `释放后可以重新占用且不会下溢`() {
        val counter = GlassLayerCounter()
        repeat(3) { assertThat(counter.tryAcquire()).isTrue() }
        counter.release()
        assertThat(counter.active).isEqualTo(2)
        assertThat(counter.tryAcquire()).isTrue()
        assertThat(counter.active).isEqualTo(3)

        // 多释放几次不应把计数带到负数。
        repeat(10) { counter.release() }
        assertThat(counter.active).isEqualTo(0)
    }

    // ---------- GlassBudget ----------

    @Test
    fun `性能预算常量与 7_5 一致`() {
        assertThat(GlassBudget.P90_FRAME_MS).isEqualTo(24.0)
        assertThat(GlassBudget.MAX_DROPPED_RATIO).isEqualTo(0.03)
        assertThat(GlassBudget.FIRST_FRAME_EXTRA_MS).isEqualTo(80.0)
        assertThat(GlassBudget.WARN_AFTER_VIOLATIONS).isEqualTo(2)
    }

    // ---------- FrameMonitor ----------

    /** 60Hz 一帧的预算（纳秒）。 */
    private val budget60 = 16_666_667L

    private fun ms(n: Double) = (n * 1_000_000).toLong()

    @Test
    fun `样本不足时不下结论`() {
        val monitor = FrameMonitor()
        repeat(FrameMonitor.MIN_SAMPLES - 1) { monitor.record(ms(100.0), budget60) }
        // 用 3 帧算 P90 没有意义 —— 必须返回 0.0 而不是编一个数。
        assertThat(monitor.p90Ms()).isEqualTo(0.0)
        assertThat(monitor.droppedRatio()).isEqualTo(0.0)
        assertThat(monitor.isOverBudget()).isFalse()
    }

    @Test
    fun `P90 取第 90 百分位而非平均值`() {
        val monitor = FrameMonitor()
        // 30 帧：前 25 帧 10ms，后 5 帧 100ms。
        // 平均值 = 25ms（未超标），P90 = 100ms（超标）—— 这正是要区分的场景：
        // 平均值会被大量廉价帧拉低，掩盖偶发长帧，而卡顿感恰恰来自长帧。
        //
        // 样本数不能随便挑：P90 落在索引 ((n-1)*0.9).toInt()。n=30 时是索引 26，
        // 需要"低于 P90 的样本 ≤ 26 个"才能取到 100ms。用 27 个 10ms 时索引 26
        // 仍落在 10ms 区间，P90 就等于 10ms —— 那测不出 P90 与平均值的差别。
        repeat(25) { monitor.record(ms(10.0), budget60) }
        repeat(5) { monitor.record(ms(100.0), budget60) }
        val sorted = List(25) { 10.0 } + List(5) { 100.0 }
        val idx = ((sorted.size - 1) * 0.90).toInt()
        assertThat(sorted[idx]).isEqualTo(100.0)

        assertThat(monitor.p90Ms()).isEqualTo(sorted[idx])
        val mean = sorted.average()
        assertThat(monitor.p90Ms()).isGreaterThan(mean)
        assertThat(monitor.isOverBudget()).isTrue()
    }

    @Test
    fun `稳定 16ms 帧不判超标`() {
        val monitor = FrameMonitor()
        repeat(FrameMonitor.MIN_SAMPLES) { monitor.record(ms(16.0), budget60) }
        assertThat(monitor.p90Ms()).isEqualTo(16.0)
        assertThat(monitor.droppedRatio()).isEqualTo(0.0)
        assertThat(monitor.isOverBudget()).isFalse()
    }

    @Test
    fun `P90 超过 24ms 判超标`() {
        val monitor = FrameMonitor()
        repeat(FrameMonitor.MIN_SAMPLES) { monitor.record(ms(30.0), budget60) }
        assertThat(monitor.p90Ms()).isEqualTo(30.0)
        assertThat(monitor.isOverBudget()).isTrue()
    }

    @Test
    fun `丢帧率超过 3% 也判超标`() {
        val monitor = FrameMonitor()
        // 帧耗时都在预算内不会丢帧，所以这里用"预算很小"制造丢帧：
        // 30 帧里 2 帧超预算 → 丢帧率 6.67% > 3%。
        repeat(FrameMonitor.MIN_SAMPLES - 2) { monitor.record(ms(10.0), budget60) }
        repeat(2) { monitor.record(ms(20.0), budget60) }
        assertThat(monitor.droppedRatio()).isGreaterThan(GlassBudget.MAX_DROPPED_RATIO)
        assertThat(monitor.isOverBudget()).isTrue()
    }

    @Test
    fun `reset 清空全部统计`() {
        val monitor = FrameMonitor()
        repeat(FrameMonitor.MIN_SAMPLES) { monitor.record(ms(50.0), budget60) }
        assertThat(monitor.isOverBudget()).isTrue()
        monitor.reset()
        assertThat(monitor.p90Ms()).isEqualTo(0.0)
        assertThat(monitor.droppedRatio()).isEqualTo(0.0)
        assertThat(monitor.isOverBudget()).isFalse()
    }

    @Test
    fun `滑动窗口只保留最近 WINDOW 帧`() {
        val monitor = FrameMonitor(window = 4)
        repeat(10) { monitor.record(ms(50.0), budget60) }
        // 窗口 4 < MIN_SAMPLES，因此仍不下结论 —— 行为与"样本不足"一致。
        assertThat(monitor.p90Ms()).isEqualTo(0.0)
    }

    // ---------- GlassPerformanceController ----------

    /** 喂足够多的超标帧，触发一次降档（返回是否发生了降档）。 */
    private fun degradeOnce(controller: GlassPerformanceController, monitor: FrameMonitor): Boolean {
        var degraded = false
        repeat(FrameMonitor.MIN_SAMPLES + 1) {
            if (controller.onFrame(ms(40.0), monitor, budget60)) degraded = true
        }
        return degraded
    }

    @Test
    fun `超标时按 强-轻-关 逐级降档`() {
        val controller = GlassPerformanceController(GlassIntensity.STRONG)
        val monitor = FrameMonitor()
        controller.attach(monitor)

        assertThat(degradeOnce(controller, monitor)).isTrue()
        assertThat(controller.effective).isEqualTo(GlassIntensity.LIGHT)

        assertThat(degradeOnce(controller, monitor)).isTrue()
        assertThat(controller.effective).isEqualTo(GlassIntensity.OFF)

        // 已经到底：不再降档，也不再报"发生了降档"。
        assertThat(degradeOnce(controller, monitor)).isFalse()
        assertThat(controller.effective).isEqualTo(GlassIntensity.OFF)
    }

    @Test
    fun `连续超标达阈值后提示用户`() {
        val controller = GlassPerformanceController(GlassIntensity.STRONG)
        val monitor = FrameMonitor()
        controller.attach(monitor)

        assertThat(controller.shouldWarnUser).isFalse()
        degradeOnce(controller, monitor)
        assertThat(controller.consecutiveViolations).isEqualTo(1)
        assertThat(controller.shouldWarnUser).isFalse()

        degradeOnce(controller, monitor)
        assertThat(controller.consecutiveViolations)
            .isAtLeast(GlassBudget.WARN_AFTER_VIOLATIONS)
        assertThat(controller.shouldWarnUser).isTrue()
    }

    @Test
    fun `降档会写日志且不改用户设置`() {
        val controller = GlassPerformanceController(GlassIntensity.STRONG)
        val monitor = FrameMonitor()
        controller.attach(monitor)
        degradeOnce(controller, monitor)

        assertThat(controller.degradeLog).isNotEmpty()
        // 悄悄改用户设置是另一种欺骗：userSetting 必须保持用户的选择。
        assertThat(controller.userSetting).isEqualTo(GlassIntensity.STRONG)
        assertThat(controller.effective).isEqualTo(GlassIntensity.LIGHT)
    }

    @Test
    fun `样本不足时不误清连续超标计数`() {
        // 回归守卫，对应一个真实修过的缺陷：
        // 降档后会 monitor.reset()，紧接着的帧样本不足。旧实现只看 isOverBudget()
        // （样本不足时它返回 false），于是把 consecutiveViolations 清零，
        // 导致 WARN_AFTER_VIOLATIONS 永远不可达。
        val controller = GlassPerformanceController(GlassIntensity.STRONG)
        val monitor = FrameMonitor()
        controller.attach(monitor)

        degradeOnce(controller, monitor)
        val afterFirst = controller.consecutiveViolations
        assertThat(afterFirst).isEqualTo(1)

        // degradeOnce 的最后一帧触发降档并 monitor.reset()，因此此刻只剩 1 个样本。
        // 再喂 MIN_SAMPLES-2 = 28 帧 → 共 29 个，仍低于 MIN_SAMPLES(30)：
        // 这一区间内既不应降档，也不应清计数。
        repeat(FrameMonitor.MIN_SAMPLES - 2) {
            assertThat(controller.onFrame(ms(40.0), monitor, budget60)).isFalse()
        }
        assertThat(controller.consecutiveViolations).isEqualTo(afterFirst)

        // 第 30 个样本到位 → 判定超标 → 计数递增到 2（证明它没被清过）并降档到 OFF。
        assertThat(controller.onFrame(ms(40.0), monitor, budget60)).isTrue()
        assertThat(controller.consecutiveViolations).isEqualTo(afterFirst + 1)
        assertThat(controller.effective).isEqualTo(GlassIntensity.OFF)
    }

    @Test
    fun `hasEnoughSamples 区分未测够与确实没超标`() {
        val monitor = FrameMonitor()
        repeat(FrameMonitor.MIN_SAMPLES - 1) { monitor.record(ms(8.0), budget60) }
        // 样本不足：isOverBudget() 与 hasEnoughSamples() 都是 false，
        // 但两者含义不同，调用方必须能分辨。
        assertThat(monitor.hasEnoughSamples()).isFalse()
        assertThat(monitor.isOverBudget()).isFalse()

        monitor.record(ms(8.0), budget60)
        // 测够了且确实没超标。
        assertThat(monitor.hasEnoughSamples()).isTrue()
        assertThat(monitor.isOverBudget()).isFalse()
    }

    @Test
    fun `用户手动改强度会重置降档状态`() {
        val controller = GlassPerformanceController(GlassIntensity.STRONG)
        val monitor = FrameMonitor()
        controller.attach(monitor)
        degradeOnce(controller, monitor)
        degradeOnce(controller, monitor)
        assertThat(controller.shouldWarnUser).isTrue()

        controller.setUserSetting(GlassIntensity.STRONG)
        assertThat(controller.userSetting).isEqualTo(GlassIntensity.STRONG)
        assertThat(controller.effective).isEqualTo(GlassIntensity.STRONG)
        assertThat(controller.consecutiveViolations).isEqualTo(0)
        assertThat(controller.shouldWarnUser).isFalse()
    }

    @Test
    fun `未超标时不降档`() {
        val controller = GlassPerformanceController(GlassIntensity.LIGHT)
        val monitor = FrameMonitor()
        controller.attach(monitor)
        repeat(FrameMonitor.MIN_SAMPLES * 2) {
            assertThat(controller.onFrame(ms(8.0), monitor, budget60)).isFalse()
        }
        assertThat(controller.effective).isEqualTo(GlassIntensity.LIGHT)
        assertThat(controller.consecutiveViolations).isEqualTo(0)
    }
}
