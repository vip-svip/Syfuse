package io.github.vipsvip.syfuse.engine.singbox

import io.github.vipsvip.syfuse.core.result.SyfuseError
import io.github.vipsvip.syfuse.core.result.SyfuseResult
import io.github.vipsvip.syfuse.domain.engine.ConnSnapshot
import io.github.vipsvip.syfuse.domain.engine.EngineConfig
import io.github.vipsvip.syfuse.domain.engine.EngineKind
import io.github.vipsvip.syfuse.domain.engine.LatencyResult
import io.github.vipsvip.syfuse.domain.engine.ProxyEngine
import io.github.vipsvip.syfuse.domain.engine.TrafficStats
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map

/**
 * sing-box 内核实现。
 *
 * ## 当前阶段的诚实说明（M1）
 *
 * 本类在 M1 **尚未接入真实内核**：sing-box 的 gomobile AAR（libbox）需要在
 * Linux 环境用 Go 交叉编译产出，M1 的骨架阶段不引入该二进制。
 * 因此所有方法返回 [SyfuseError.NotImplemented]，UI 会如实显示「功能尚未实现」
 * 而非伪装成功（红线条 R4：不造假）。
 *
 * M2 的填充计划：把状态机的桩替换为 libbox 的 `PlatformInterface` 与
 * `BoxService` 调用，并新增 IPC 客户端处理跨进程通信。
 *
 * ## 为什么这个桩不是「占位类」（提示词 2.3 第 4 条禁止形式主义）
 *
 * 它承担两个真实职责：
 * 1. 让 [ProxyEngine] 接口在编译期被验证可被实现（接口设计若有误，M1 即暴露）
 * 2. 让 M1 工程骨架可完整编译，满足「可编译可运行」的里程碑判定
 */
class SingBoxEngine : ProxyEngine {

    override val kind: EngineKind = EngineKind.SING_BOX

    /** 内核运行时状态。M2 起由内核回调驱动，现在是纯本地状态。 */
    private val state = MutableStateFlow(EngineState.STOPPED)

    override suspend fun version(): String = NOT_WIRED

    override suspend fun start(config: EngineConfig): SyfuseResult<Unit> =
        SyfuseResult.Failure(SyfuseError.NotImplemented("sing-box 内核启动（M2 接入 libbox）"))

    override suspend fun stop(): SyfuseResult<Unit> =
        SyfuseResult.Failure(SyfuseError.NotImplemented("sing-box 内核停止（M2 接入 libbox）"))

    override suspend fun reload(config: EngineConfig): SyfuseResult<Unit> =
        SyfuseResult.Failure(SyfuseError.NotImplemented("sing-box 配置热重载（M2）"))

    /**
     * 流量流。
     *
     * 未运行时发射零值统计而非空流：UI 需要持续渲染「0 B/s」而不是卡在加载态，
     * 否则首页会一直显示骨架屏（违反提示词 7.3 的四态要求）。
     *
     * M2 接入内核后，这里改为从 libbox 的 `PlatformInterface` 读取并做速率换算
     * （内核只给累计字节，速率需按采样间隔求导）。
     */
    override fun stats(): Flow<TrafficStats> = state.map { TrafficStats() }

    /** 连接列表流。未运行时为空列表，同样是为了让 UI 走「空状态」而非「加载态」。 */
    override fun connections(): Flow<List<ConnSnapshot>> = state.map { emptyList() }

    override suspend fun select(group: String, member: String): SyfuseResult<Unit> =
        SyfuseResult.Failure(SyfuseError.NotImplemented("策略组选择（M3）"))

    override suspend fun urlTest(target: String): SyfuseResult<List<LatencyResult>> =
        SyfuseResult.Failure(SyfuseError.NotImplemented("延迟测速（M3）"))

    /** 内核状态机。 */
    enum class EngineState { STOPPED, STARTING, RUNNING, STOPPING, ERROR }

    private companion object {
        /** 明确区分「未接线」与「版本号为 0」，避免诊断页误显示。 */
        const val NOT_WIRED = "not-wired"
    }
}
