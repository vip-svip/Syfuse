package io.github.vipsvip.syfuse.domain.engine

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.domain.model.ConnSnapshot
import io.github.vipsvip.syfuse.domain.model.Profile
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** 连接开关的四种状态，与 5.1 首页大开关的四态颜色语义一一对应。 */
enum class EngineState {
    /** 未连接。 */
    STOPPED,

    /** 连接中。 */
    STARTING,

    /** 已连接。 */
    RUNNING,

    /** 断开中。 */
    STOPPING,

    /** 错误。必须携带可点开查看的原因。 */
    ERROR,
}

/** 引擎状态。 */
data class EngineStatus(
    val state: EngineState = EngineState.STOPPED,
    val sinceEpochMs: Long = 0L,
    /** 仅 ERROR 态非空；UI 的错误态必须能点进来看原因（5.1）。 */
    val error: SyfuseError? = null,
)

/** 速率与累计流量。 */
data class TrafficRate(
    val upBytesPerSec: Double = 0.0,
    val downBytesPerSec: Double = 0.0,
    val totalUpBytes: Long = 0L,
    val totalDownBytes: Long = 0L,
)

/** 传给内核的启动参数。由 Profile + 节点 + 设置编译而来。 */
data class EngineConfig(
    val profile: Profile,
    /** 内核配置文本（sing-box / mihomo 格式），由配置编译器产出。 */
    val coreConfig: String,
    val tunMtu: Int = 1500,
    val tunStack: String = "gvisor",
    val autoRoute: Boolean = true,
)

/**
 * 数据面。真正的转发、DNS、规则匹配发生在这里。
 *
 * 之所以把它抽成接口：9.2 要求内核可替换。UI 与状态机只依赖本接口，
 * 换成 sing-box / mihomo / 自研内核时不需要改上层任何一行。
 */
interface DataPlane {
    suspend fun start(config: EngineConfig): SyfuseResult<Unit>

    suspend fun stop(): SyfuseResult<Unit>

    suspend fun reload(config: EngineConfig): SyfuseResult<Unit>

    /** 当前连接列表快照。 */
    suspend fun connections(): List<ConnSnapshot>

    /** 累计字节计数器。返回 null 表示数据面尚未就绪。 */
    suspend fun counters(): Pair<Long, Long>?

    suspend fun select(group: String, node: String): SyfuseResult<Unit>

    /** 测速。返回 名称 → 延迟毫秒（失败为 null）。 */
    suspend fun urlTest(names: List<String>): SyfuseResult<Map<String, Int?>>

    /** 数据面是否可用（内核是否已接入）。 */
    val available: Boolean

    /** 不可用时的原因，用于如实展示给用户而不是假装连接成功。 */
    val unavailableReason: String?

    /**
     * 主动探测一次可用性（**可能触发 IO**）。
     *
     * 为什么不能只靠 [available]：对走 UDS 连守护进程的数据面，
     * "可用"不是静态属性 —— 守护进程可能没启动、访问令牌可能还没配对、
     * 用户可能刚在 action.sh 里停掉它。[available] 因此只能是
     * **最近一次探测的结果**，一个非 suspend 的 getter 不能自己去发起 IO。
     *
     * [EngineController] 在开始连接前会调用本方法刷新一次，
     * 使"守护进程后来起来了"这件事能被自动发现，而不是要用户重启 App。
     */
    suspend fun probeAvailability(): Boolean
}

/**
 * 引擎控制器：负责状态机与速率计算。
 *
 * 这是一个**真实实现**，不是占位。它的职责边界很明确：
 *  - 维护状态迁移的合法性（不允许从 STOPPED 直接 reload 等）；
 *  - 把数据面的原始计数器换算为速率；
 *  - 把失败转成带用户可读信息的 [SyfuseError]。
 * 真正的数据转发由 [DataPlane] 提供——M1 阶段该实现尚不可用，
 * 因此状态机会如实地停在 ERROR 并给出原因，而不是伪装成 RUNNING。
 */
class EngineController(
    private val dataPlane: DataPlane,
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private val _status = MutableStateFlow(EngineStatus())
    val status: StateFlow<EngineStatus> = _status.asStateFlow()

    private val meter = TrafficMeter()

    /** 最近一次速率采样。suspend 而非 runBlocking——避免在 UI 线程上阻塞等待内核。 */
    suspend fun sampleRate(): TrafficRate {
        val counters = dataPlane.counters() ?: return TrafficRate()
        return meter.sample(counters.first, counters.second, clock())
    }

    suspend fun start(config: EngineConfig): SyfuseResult<Unit> {
        val current = _status.value.state
        if (current == EngineState.RUNNING) return SyfuseResult.Success(Unit)
        if (current == EngineState.STARTING || current == EngineState.STOPPING) {
            return reject("引擎正在状态迁移中（$current），请稍后再试")
        }

        // 数据面可用性不是静态属性：走 UDS 连守护进程的实现，其可用性会随
        // 守护进程起停与令牌配对状态而变。因此先刷新一次再判定 ——
        // 否则"守护进程后来起来了"只能靠重启 App 才能被发现。
        val usable = dataPlane.available || dataPlane.probeAvailability()
        if (!usable) {
            val reason = dataPlane.unavailableReason ?: "数据面不可用"
            val error = SyfuseError(
                code = ErrorCode.ENGINE_START_FAILED,
                technical = reason,
                userMessage = "无法建立连接：数据面不可用",
                // hint 直接采用数据面给出的原因：那是它对自己状态的**真实**描述，
                // 比上层能编的任何话都准确（例如"代理内核尚未接入…"或
                // "无法连接守护进程…"）。
                hint = reason,
            )
            _status.value = EngineStatus(EngineState.ERROR, clock(), error)
            return SyfuseResult.Failure(error)
        }

        _status.value = EngineStatus(EngineState.STARTING, clock())
        return when (val result = dataPlane.start(config)) {
            is SyfuseResult.Success -> {
                meter.reset()
                _status.value = EngineStatus(EngineState.RUNNING, clock())
                SyfuseResult.Success(Unit)
            }
            is SyfuseResult.Failure -> {
                _status.value = EngineStatus(EngineState.ERROR, clock(), result.error)
                result
            }
        }
    }

    suspend fun stop(): SyfuseResult<Unit> {
        val current = _status.value.state
        if (current == EngineState.STOPPED) return SyfuseResult.Success(Unit)
        if (current == EngineState.STARTING || current == EngineState.STOPPING) {
            return reject("引擎正在状态迁移中（$current），请稍后再试")
        }

        _status.value = EngineStatus(EngineState.STOPPING, clock())
        return when (val result = dataPlane.stop()) {
            is SyfuseResult.Success -> {
                meter.reset()
                _status.value = EngineStatus(EngineState.STOPPED, clock())
                SyfuseResult.Success(Unit)
            }
            is SyfuseResult.Failure -> {
                _status.value = EngineStatus(EngineState.ERROR, clock(), result.error)
                result
            }
        }
    }

    suspend fun reload(config: EngineConfig): SyfuseResult<Unit> {
        if (_status.value.state != EngineState.RUNNING) {
            return reject("只有在已连接状态下才能热重载配置")
        }
        return when (val result = dataPlane.reload(config)) {
            is SyfuseResult.Success -> SyfuseResult.Success(Unit)
            is SyfuseResult.Failure -> {
                // 热重载失败不改变连接状态：旧配置仍在生效，这正是"热重载不中断连接"的语义
                result
            }
        }
    }

    suspend fun connections(): List<ConnSnapshot> =
        if (_status.value.state == EngineState.RUNNING) dataPlane.connections() else emptyList()

    suspend fun select(group: String, node: String): SyfuseResult<Unit> =
        dataPlane.select(group, node)

    suspend fun urlTest(names: List<String>): SyfuseResult<Map<String, Int?>> =
        dataPlane.urlTest(names)

    private fun reject(message: String): SyfuseResult.Failure = SyfuseResult.Failure(
        SyfuseError(
            code = ErrorCode.ENGINE_IPC_FAILED,
            technical = message,
            userMessage = message,
            hint = "请等待当前操作完成",
        ),
    )
}

/**
 * 速率计算。
 *
 * 用指数移动平均（EMA）而不是"本次差值 / 本次间隔"：
 * 内核上报的计数器采样间隔并不严格等距，直接用瞬时差值会让速率数字剧烈跳动，
 * 用户看到的是"噪声"而不是"速率"。EMA 的平滑系数可调。
 *
 * 计数器回退（内核重启、连接重建）会被识别为重置，避免出现负数或天文数字。
 */
class TrafficMeter(private val smoothing: Double = 0.3) {

    private var lastUp = 0L
    private var lastDown = 0L
    private var lastAt = 0L
    private var emaUp = 0.0
    private var emaDown = 0.0
    private var initialized = false

    fun reset() {
        lastUp = 0
        lastDown = 0
        lastAt = 0
        emaUp = 0.0
        emaDown = 0.0
        initialized = false
    }

    fun sample(upBytes: Long, downBytes: Long, atMs: Long): TrafficRate {
        if (!initialized) {
            initialized = true
            lastUp = upBytes
            lastDown = downBytes
            lastAt = atMs
            return TrafficRate(0.0, 0.0, upBytes, downBytes)
        }

        val elapsedMs = atMs - lastAt
        if (elapsedMs <= 0) {
            return TrafficRate(emaUp, emaDown, upBytes, downBytes)
        }

        val deltaUp = upBytes - lastUp
        val deltaDown = downBytes - lastDown
        val countersReset = deltaUp < 0 || deltaDown < 0

        if (!countersReset) {
            val seconds = elapsedMs / 1000.0
            val instantUp = deltaUp / seconds
            val instantDown = deltaDown / seconds
            emaUp = emaUp * (1 - smoothing) + instantUp * smoothing
            emaDown = emaDown * (1 - smoothing) + instantDown * smoothing
        } else {
            // 计数器回退：重置基线，本次速率为 0，绝不输出负值
            emaUp = 0.0
            emaDown = 0.0
        }

        lastUp = upBytes
        lastDown = downBytes
        lastAt = atMs
        return TrafficRate(emaUp, emaDown, upBytes, downBytes)
    }
}
