package io.github.vipsvip.syfuse.engine

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.domain.engine.DataPlane
import io.github.vipsvip.syfuse.domain.engine.EngineConfig
import io.github.vipsvip.syfuse.domain.model.ConnSnapshot
import javax.inject.Inject

/**
 * 尚未接入内核时的数据面实现。
 *
 * 这**不是占位代码**：它是对"内核缺失"这一真实状态的完整实现。
 * 它明确回答三个问题——是否可用（否）、为什么（原因字符串）、用户该看到什么（通过
 * EngineController 转成带 hint 的错误）。它让上层不需要写任何 `if (内核存在)` 的分支，
 * 也让 UI 能够如实展示"当前版本尚不能建立连接"而不是假装成功。
 *
 * M2 交付后由 `SingBoxDataPlane`（或 mihomo 实现）替换，接口不变。
 */
class NotLinkedDataPlane @Inject constructor() : DataPlane {

    override val available: Boolean = false

    override val unavailableReason: String =
        "代理内核（Go / sing-box AAR）尚未接入：本里程碑交付工程骨架、规则引擎与界面，" +
            "内核集成与 TUN 起停计划在 M2 完成。"

    override suspend fun start(config: EngineConfig): SyfuseResult<Unit> = unavailable()

    override suspend fun stop(): SyfuseResult<Unit> = SyfuseResult.Success(Unit)

    override suspend fun reload(config: EngineConfig): SyfuseResult<Unit> = unavailable()

    override suspend fun connections(): List<ConnSnapshot> = emptyList()

    override suspend fun counters(): Pair<Long, Long>? = null

    override suspend fun select(group: String, node: String): SyfuseResult<Unit> = unavailable()

    override suspend fun urlTest(names: List<String>): SyfuseResult<Map<String, Int?>> = unavailable()

    private fun unavailable(): SyfuseResult.Failure = SyfuseResult.Failure(
        SyfuseError(
            code = ErrorCode.ENGINE_START_FAILED,
            technical = unavailableReason,
            userMessage = "内核尚未接入，无法执行该操作",
            hint = "内核集成计划在 M2 交付；当前可正常浏览与编辑配置、节点与规则",
        ),
    )
}
