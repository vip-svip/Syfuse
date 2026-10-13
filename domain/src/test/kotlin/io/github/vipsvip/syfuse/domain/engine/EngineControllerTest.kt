package io.github.vipsvip.syfuse.domain.engine

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.domain.model.ConnSnapshot
import io.github.vipsvip.syfuse.domain.model.Profile
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Test

/** 测试替身：可控可用性与行为的假数据面。 */
private class FakeDataPlane(
    override val available: Boolean = true,
    override val unavailableReason: String? = null,
    private val startResult: SyfuseResult<Unit> = SyfuseResult.Success(Unit),
    private val stopResult: SyfuseResult<Unit> = SyfuseResult.Success(Unit),
    /** 探测结果。默认与 available 一致，即"探测不会改变结论"。 */
    private val probeResult: Boolean = available,
) : DataPlane {
    var startCount = 0
    var stopCount = 0
    var probeCount = 0
    var lastConfig: EngineConfig? = null
    var countersValue: Pair<Long, Long>? = 0L to 0L

    override suspend fun probeAvailability(): Boolean {
        probeCount++
        return probeResult
    }

    override suspend fun start(config: EngineConfig): SyfuseResult<Unit> {
        startCount++
        lastConfig = config
        return startResult
    }

    override suspend fun stop(): SyfuseResult<Unit> {
        stopCount++
        return stopResult
    }

    override suspend fun reload(config: EngineConfig): SyfuseResult<Unit> = SyfuseResult.Success(Unit)

    override suspend fun connections(): List<ConnSnapshot> = emptyList()

    override suspend fun counters(): Pair<Long, Long>? = countersValue

    override suspend fun select(group: String, node: String): SyfuseResult<Unit> = SyfuseResult.Success(Unit)

    override suspend fun urlTest(names: List<String>): SyfuseResult<Map<String, Int?>> =
        SyfuseResult.Success(names.associateWith { 42 })
}

private fun config() = EngineConfig(profile = Profile(id = "p", name = "p"), coreConfig = "{}")

class EngineControllerTest {

    @Test
    fun `内核不可用时状态停在 ERROR 且不伪装为已连接`() = runTest {
        val plane = FakeDataPlane(available = false, unavailableReason = "内核 AAR 未接入")
        val controller = EngineController(plane) { 1000L }

        val result = controller.start(config())

        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        assertThat(error.code).isEqualTo(ErrorCode.ENGINE_START_FAILED)
        // 错误态必须能点开看原因（5.1）：userMessage 与 hint 都不能为空
        assertThat(error.userMessage).isNotEmpty()
        assertThat(error.hint).isNotNull()
        assertThat(controller.status.value.state).isEqualTo(EngineState.ERROR)
        // 关键：没有真的去调数据面
        assertThat(plane.startCount).isEqualTo(0)
    }

    @Test
    fun `缓存为不可用时会先探测一次再判定`() = runTest {
        // 模拟"守护进程后来起来了"：available 还是旧的 false，但探测能成功。
        // 没有这一步，用户就必须重启 App 才能连上 —— 那是不可接受的。
        val plane = FakeDataPlane(available = false, probeResult = true)
        val controller = EngineController(plane) { 3000L }

        val result = controller.start(config())

        assertThat(result).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat(plane.probeCount).isEqualTo(1)
        assertThat(plane.startCount).isEqualTo(1)
        assertThat(controller.status.value.state).isEqualTo(EngineState.RUNNING)
    }

    @Test
    fun `已可用时不做多余探测`() = runTest {
        val plane = FakeDataPlane(available = true)
        EngineController(plane) { 0L }.start(config())

        assertThat(plane.probeCount).isEqualTo(0)
    }

    @Test
    fun `可用时正常迁移到 RUNNING`() = runTest {
        val plane = FakeDataPlane()
        val controller = EngineController(plane) { 2000L }

        val result = controller.start(config())

        assertThat(result).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat(controller.status.value.state).isEqualTo(EngineState.RUNNING)
        assertThat(controller.status.value.sinceEpochMs).isEqualTo(2000L)
        assertThat(plane.startCount).isEqualTo(1)
    }

    @Test
    fun `重复 start 在已连接时幂等`() = runTest {
        val plane = FakeDataPlane()
        val controller = EngineController(plane) { 0L }
        controller.start(config())
        controller.start(config())

        assertThat(plane.startCount).isEqualTo(1)
    }

    @Test
    fun `未连接时 reload 被拒绝而不是静默失败`() = runTest {
        val controller = EngineController(FakeDataPlane()) { 0L }

        val result = controller.reload(config())

        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        assertThat((result as SyfuseResult.Failure).error.code).isEqualTo(ErrorCode.ENGINE_IPC_FAILED)
    }

    @Test
    fun `stop 在未连接时幂等且不报错`() = runTest {
        val plane = FakeDataPlane()
        val controller = EngineController(plane) { 0L }

        val result = controller.stop()

        assertThat(result).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat(plane.stopCount).isEqualTo(0)
    }

    @Test
    fun `启动失败时状态转为 ERROR 并带上原始错误`() = runTest {
        val failure = SyfuseResult.Failure(
            SyfuseError(ErrorCode.VPN_PERMISSION_DENIED, "user denied", "用户拒绝了 VPN 授权"),
        )
        val controller = EngineController(FakeDataPlane(startResult = failure)) { 0L }

        val result = controller.start(config())

        assertThat((result as SyfuseResult.Failure).error.code)
            .isEqualTo(ErrorCode.VPN_PERMISSION_DENIED)
        assertThat(controller.status.value.state).isEqualTo(EngineState.ERROR)
    }

    @Test
    fun `未连接时连接列表为空而不抛异常`() = runTest {
        val controller = EngineController(FakeDataPlane()) { 0L }
        assertThat(controller.connections()).isEmpty()
    }
}

class TrafficMeterTest {

    @Test
    fun `首次采样只建立基线速率为零`() {
        val meter = TrafficMeter()
        val rate = meter.sample(upBytes = 1000, downBytes = 2000, atMs = 10_000)
        assertThat(rate.upBytesPerSec).isEqualTo(0.0)
        assertThat(rate.downBytesPerSec).isEqualTo(0.0)
        assertThat(rate.totalUpBytes).isEqualTo(1000)
    }

    @Test
    fun `等间隔采样的速率收敛到真实值`() {
        val meter = TrafficMeter(smoothing = 1.0) // 取 1.0 便于断言精确值
        meter.sample(0, 0, 0)
        val rate = meter.sample(upBytes = 1000, downBytes = 4000, atMs = 1000)
        assertThat(rate.upBytesPerSec).isWithin(1e-6).of(1000.0)
        assertThat(rate.downBytesPerSec).isWithin(1e-6).of(4000.0)
    }

    @Test
    fun `计数器回退被识别为重置且不产生负速率`() {
        val meter = TrafficMeter(smoothing = 1.0)
        meter.sample(10_000, 20_000, 0)
        val rate = meter.sample(upBytes = 5, downBytes = 7, atMs = 1000)
        assertThat(rate.upBytesPerSec).isEqualTo(0.0)
        assertThat(rate.downBytesPerSec).isEqualTo(0.0)
    }

    @Test
    fun `时间未推进时沿用上次速率`() {
        val meter = TrafficMeter(smoothing = 1.0)
        meter.sample(0, 0, 0)
        meter.sample(1000, 0, 1000)
        val rate = meter.sample(2000, 0, 1000)
        assertThat(rate.upBytesPerSec).isWithin(1e-6).of(1000.0)
    }

    @Test
    fun `reset 后重新建立基线`() {
        val meter = TrafficMeter()
        meter.sample(1000, 1000, 0)
        meter.reset()
        val rate = meter.sample(9999, 9999, 1000)
        assertThat(rate.upBytesPerSec).isEqualTo(0.0)
        assertThat(rate.totalUpBytes).isEqualTo(9999)
    }
}
