package io.github.vipsvip.syfuse.engine

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonClient
import io.github.vipsvip.syfuse.domain.engine.DataPlane
import io.github.vipsvip.syfuse.domain.engine.EngineConfig
import io.github.vipsvip.syfuse.domain.model.ConnSnapshot
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.put
import javax.inject.Inject

/**
 * 走 UDS 连接守护进程（syfused）的数据面。
 *
 * **这是 M3「内核打通」的落点**：它把 App 与守护进程真正连起来 ——
 * 在此之前，`DataPlane` 只有一个"如实报错"的实现，App 与模块之间
 * 没有任何一条活链路。
 *
 * 诚实边界（必须写清楚，否则就成了"假装内核已接入"）：
 *
 *  1. **代理隧道本身尚未实现。** [start] 把请求转发给守护进程的
 *     `proxies.start`，而守护进程把它注册为"未实现（计划里程碑 M3）"。
 *     因此 [start] 会拿到一个**可展示的真实原因**并如实失败 ——
 *     不会返回成功、不会让界面显示"已连接"。这条转发链路本身是有价值的：
 *     它证明了 App → UDS → 守护进程 → 应答 的往返是通的。
 *  2. **规则能力已经可用。** `rules.test` / `rules.syntax` / `rules.stats`
 *     是真实实现的，可以直接调用（见 [ruleSetStats]、[ruleDiagnostics]）。
 *  3. **无 root 分支未实现。** 设备没有 root 时，App 应当改为进程内加载
 *     内核（gomobile AAR）。那条分支尚未交付，当前实现会因读不到令牌
 *     而如实报告"尚未获取访问令牌"。
 */
class UdsDataPlane @Inject constructor(
    private val client: DaemonClient,
) : DataPlane {

    /** 最近一次探测的结果。 */
    private sealed interface Probe {
        /** 从未探测过。 */
        data object Unknown : Probe

        /** 探测成功。 */
        data class Ready(val daemonVersion: String) : Probe

        /** 探测失败，携带可展示的原因。 */
        data class Unavailable(val reason: String) : Probe
    }

    @Volatile
    private var probe: Probe = Probe.Unknown

    /**
     * 本数据面是否**成功启动过**隧道。
     *
     * 用途：在隧道尚未实现（[start] 必然失败）时，[stop] 不应该再向守护进程
     * 发一次注定失败的 `proxies.stop` 并把引擎推进 ERROR ——
     * "没启动过的东西停下来"本来就是成功的。这个标志是本地事实，
     * 不是对守护进程状态的猜测。
     */
    @Volatile
    private var startedByUs: Boolean = false

    override val available: Boolean get() = probe is Probe.Ready

    override val unavailableReason: String?
        get() = when (val p = probe) {
            is Probe.Ready -> null
            is Probe.Unavailable -> p.reason
            Probe.Unknown ->
                "尚未与守护进程通信过。App 会在开始连接前自动探测一次；" +
                    "若探测失败，原因会显示在这里。"
        }

    /** 已探明的守护进程版本；未探测成功时为 null。 */
    val daemonVersion: String? get() = (probe as? Probe.Ready)?.daemonVersion

    /**
     * 探测一次可用性。
     *
     * 以 `status` 为准：它能同时验证"socket 能连上"、"令牌正确"、
     * "协议版本匹配"这三件事 —— 只 ping socket 是证明不了的。
     */
    override suspend fun probeAvailability(): Boolean {
        probe = when (val result = client.call("status")) {
            is SyfuseResult.Success -> Probe.Ready(daemonVersionOf(result.value))
            is SyfuseResult.Failure -> Probe.Unavailable(describe(result.error))
        }
        return available
    }

    override suspend fun start(config: EngineConfig): SyfuseResult<Unit> {
        // 把内核配置整体交给守护进程。守护进程当前会如实回"未实现（M3）"，
        // 这条应答会被原样带回上层展示 —— 不吞掉、不美化、不假装成功。
        val result = client.call(
            "proxies.start",
            buildJsonObject {
                put("profileId", config.profile.id)
                put("profileName", config.profile.name)
                put("coreConfig", config.coreConfig)
                put("tunMtu", config.tunMtu)
                put("tunStack", config.tunStack)
                put("autoRoute", config.autoRoute)
            },
        )
        return when (result) {
            is SyfuseResult.Success -> {
                startedByUs = true
                SyfuseResult.Success(Unit)
            }
            is SyfuseResult.Failure -> result
        }
    }

    override suspend fun stop(): SyfuseResult<Unit> {
        if (!startedByUs) {
            // 没有启动过隧道 → 停止是成功的空操作，不需要打扰守护进程。
            return SyfuseResult.Success(Unit)
        }
        return when (val result = client.call("proxies.stop")) {
            is SyfuseResult.Success -> {
                startedByUs = false
                SyfuseResult.Success(Unit)
            }
            is SyfuseResult.Failure -> result
        }
    }

    override suspend fun reload(config: EngineConfig): SyfuseResult<Unit> =
        when (
            val result = client.call(
                "proxies.reload",
                buildJsonObject { put("coreConfig", config.coreConfig) },
            )
        ) {
            is SyfuseResult.Success -> SyfuseResult.Success(Unit)
            is SyfuseResult.Failure -> result
        }

    /**
     * 连接列表。
     *
     * 守护进程的 `connections` 属 M9（未实现），因此这里如实返回空列表，
     * 且**不做**一次注定失败的网络往返 —— 那只会浪费一次 IPC 而不产生信息。
     * 接口没有错误通道，所以"为什么是空的"由能力判定
     * （`DaemonCapability.pendingMilestone("connections")`）负责呈现：
     * 界面据此把入口标为"实验性/不可用"，而不是显示一个空列表让用户
     * 以为"当前没有连接"。
     */
    override suspend fun connections(): List<ConnSnapshot> = emptyList()

    /** 累计计数器。守护进程的 `traffic` 属 M9，未就绪时按接口约定返回 null。 */
    override suspend fun counters(): Pair<Long, Long>? = null

    override suspend fun select(group: String, node: String): SyfuseResult<Unit> =
        when (
            val result = client.call(
                "proxies.select",
                buildJsonObject {
                    put("group", group)
                    put("node", node)
                },
            )
        ) {
            is SyfuseResult.Success -> SyfuseResult.Success(Unit)
            is SyfuseResult.Failure -> result
        }

    override suspend fun urlTest(names: List<String>): SyfuseResult<Map<String, Int?>> {
        // 测速需要守护进程真的能建连（M4）。当前如实返回失败，并带上真实原因。
        val result = client.call("proxies.urlTest")
        return when (result) {
            is SyfuseResult.Success -> SyfuseResult.Success(emptyMap())
            is SyfuseResult.Failure -> result
        }
    }

    // ---------------------------------------------------------------- 规则能力

    /**
     * 规则集统计。这是**真实可用**的能力（M3 已交付），
     * 与 `rules.test` 一起构成"App 侧能查规则、模块侧能算规则"的闭环。
     */
    suspend fun ruleSetStats(): SyfuseResult<JsonObject> =
        client.rulesStats().requireObject()

    /** 规则体检：带行号的逐条诊断。 */
    suspend fun ruleDiagnostics(): SyfuseResult<JsonObject> =
        client.rulesSyntax().requireObject()

    /**
     * 规则测试：对单个请求跑一次匹配，拿回决策与完整候选链路。
     */
    suspend fun testRule(
        host: String? = null,
        url: String? = null,
        resourceType: String? = null,
        documentHost: String? = null,
    ): SyfuseResult<JsonObject> = client.rulesTest(
        host = host,
        url = url,
        resourceType = resourceType,
        documentHost = documentHost,
        collectChain = true,
    ).requireObject()

    // ---------------------------------------------------------------- 内部

    private fun SyfuseResult<kotlinx.serialization.json.JsonElement>.requireObject(): SyfuseResult<JsonObject> =
        when (this) {
            is SyfuseResult.Success -> {
                val obj = value as? JsonObject
                if (obj == null) {
                    SyfuseResult.Failure(
                        SyfuseError(
                            code = ErrorCode.ENGINE_IPC_FAILED,
                            technical = "not_a_json_object: $value",
                            userMessage = "守护进程返回了非预期的内容格式",
                            hint = "App 与模块版本可能不匹配",
                        ),
                    )
                } else {
                    SyfuseResult.Success(obj)
                }
            }
            is SyfuseResult.Failure -> this
        }

    private fun daemonVersionOf(element: kotlinx.serialization.json.JsonElement): String {
        val obj = element as? JsonObject ?: return "未知"
        val v = (obj["daemonVersion"] as? JsonPrimitive)?.contentOrNull
        return v ?: "未知"
    }

    /** 把结构化错误压成一行可展示的原因（userMessage 为主，hint 补充）。 */
    private fun describe(error: SyfuseError): String {
        val hint = error.hint?.takeIf { it.isNotBlank() }
        return if (hint == null) error.userMessage else "${error.userMessage}（$hint）"
    }
}
