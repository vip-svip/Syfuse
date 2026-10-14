package io.github.vipsvip.syfuse.engine

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonClient
import io.github.vipsvip.syfuse.domain.engine.DataPlane
import io.github.vipsvip.syfuse.domain.engine.EngineConfig
import io.github.vipsvip.syfuse.domain.model.ConnSnapshot
import io.github.vipsvip.syfuse.domain.model.ConnState
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
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
 *  1. **proxies.* 已在守护进程侧真实实现**（M3/M5）：start 会真的建立
 *     SOCKS5 入站 + 路由决策 + 出站拨号，应答带 state=running 与真实
 *     socksAddr；kernelLinked 是 JSON **布尔**字面量（不是字符串 "true"）。
 *     仍未做的是 Android 侧 VpnService TUN 入站（需真机，见 PENDING-EXTERNAL.md）
 *     ——它不改变"隧道本身已在工作"这一事实。
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
     * 本数据面是否**当前持有一条由我们启动的隧道**。
     *
     * 用途：没启动过就 [stop]，是成功的空操作——不该向守护进程发一次
     * 多余的 `proxies.stop`，也不该把引擎推进 ERROR。[start] 成功置位、
     * [stop] 成功复位；App 进程重启后它归零，而守护进程侧的真实状态
     * 由 `proxies.status` 如实呈现，两者不互相冒充。
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
        // 参数名与守护进程侧 proxyStartParams 对齐（configJson / profileId / node）。
        // 用 configJson 而不是逐字段传：配置包是一份完整文档，逐字段拆开再拼回去
        // 会在两侧各维护一套字段映射，任一侧加字段就漂移。
        val result = client.call(
            "proxies.start",
            buildJsonObject {
                put("configJson", config.coreConfig)
                put("profileId", config.profile.id)
            },
        )
        return when (result) {
            is SyfuseResult.Success -> {
                // 守护进程如实回 state。只有真的 running 才算启动成功 ——
                // 把 not_linked 当成功会让界面显示"已连接"，而流量根本没被代理。
                val obj = result.value as? JsonObject
                val state = obj?.stringOrNull("state")
                val linked = obj?.boolOrNull("kernelLinked") == true
                if (state == "failed") {
                    val err = obj?.stringOrNull("error")
                    SyfuseResult.Failure(
                        SyfuseError(
                            code = ErrorCode.ENGINE_START_FAILED,
                            technical = "proxies.start state=failed: ${err ?: "未提供原因"}",
                            userMessage = "内核拒绝了这份配置：${err ?: "原因未提供"}",
                            hint = "请在「规则」页用规则体检确认配置，或换个节点再试",
                        ),
                    )
                } else if (!linked) {
                    // 内核宿主未接通：这是**如实失败**，不是"已连接"。
                    val note = obj?.stringOrNull("note")
                    SyfuseResult.Failure(
                        SyfuseError(
                            code = ErrorCode.ENGINE_IPC_FAILED,
                            technical = "proxies.start state=$state, kernelLinked=false",
                            userMessage = "配置已就绪，但内核宿主尚未接通",
                            hint = note ?: "内核正在装载中；完成后重试即可开始代理",
                        ),
                    )
                } else {
                    startedByUs = true
                    SyfuseResult.Success(Unit)
                }
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
                buildJsonObject {
                    put("configJson", config.coreConfig)
                    put("profileId", config.profile.id)
                },
            )
        ) {
            is SyfuseResult.Success -> SyfuseResult.Success(Unit)
            is SyfuseResult.Failure -> result
        }

    /**
     * 连接列表快照。
     *
     * 数据来自守护进程的 `connections`（M9 已实现）：内核数据面对每条
     * 真实连接登记字节数与生命周期（internal/conn.Tracker），这里只做
     * 字段映射。字段缺失一律落 null / 0，**不补任何看起来合理的默认值**；
     * 一次 IPC 失败如实回空列表——调用方（EngineController）只在
     * RUNNING 态查询，空列表在此处与"当前没有连接"同义。
     *
     * 方向约定（与 internal/conn.Record 的注释一致）：
     *  BytesIn = 客户端→上游 = upBytes；BytesOut = 上游→客户端 = downBytes。
     */
    override suspend fun connections(): List<ConnSnapshot> {
        val result = client.call("connections")
        val obj = (result as? SyfuseResult.Success)?.value as? JsonObject ?: return emptyList()
        val records = obj["records"] as? JsonArray ?: return emptyList()
        return records.mapNotNull { el ->
            val rec = el as? JsonObject ?: return@mapNotNull null
            val id = rec.numberOrNull("id") ?: return@mapNotNull null
            // Go 侧 time.Time 零值序列化为 "0001-01-01T00:00:00Z"（epoch 为负），
            // 含义是"仍活动"——判活动必须用 >0，而不是判字段存在。
            val startMs = rec.stringOrNull("startTime")?.toEpochMillis() ?: return@mapNotNull null
            val closed = rec.stringOrNull("endTime")?.toEpochMillis()?.takeIf { it > 0 }
            val reason = rec.stringOrNull("closeReason")
            ConnSnapshot(
                id = id.toString(),
                // Go 侧 protocol 目前恒为 "tcp"（内核只有 TCP 入站）；缺失时不猜。
                network = rec.stringOrNull("protocol") ?: "",
                // type：连接类型（proxy/direct）。内核 labels 里记的是路由
                // 决策 "decision"，值域 proxy/direct/…——这是语义最近的一个，
                // 没有则留空串，由界面显示"未上报"。
                type = labelsOf(rec)["decision"] ?: "",
                srcApp = null, // UID→包名归属需要真机 TrafficStats（M9 真机项），不编造。
                dstHost = rec.stringOrNull("dstHost"),
                dstIp = null, // 拨号按主机名走，内核侧没有独立的目标 IP 字段；不拿 srcAddr 冒充。
                ruleText = null, // 规则原文回填待 M5 数据面对接（见 RuleRouter 字段取舍注释）。
                chain = null,
                upBytes = rec.numberOrNull("bytesIn") ?: 0L,
                downBytes = rec.numberOrNull("bytesOut") ?: 0L,
                startAt = startMs,
                state = when {
                    closed == null -> ConnState.ACTIVE
                    reason == "timeout" || reason == "shutdown" || reason.isNullOrEmpty() -> ConnState.CLOSED
                    else -> ConnState.CLOSED
                },
            )
        }
    }

    /**
     * 累计字节计数器（traffic 的聚合视图，与 connections 严格同源）。
     *
     * 返回 null 的唯一含义是"这一轮拿不到数据"（IPC 失败 / 应答不是预期
     * 对象），[EngineController.sampleRate] 据此回落到空速率；
     * 引擎未启动时守护进程返回全零统计，那是真实的"零流量"。
     */
    override suspend fun counters(): Pair<Long, Long>? {
        val result = client.call("traffic")
        val obj = (result as? SyfuseResult.Success)?.value as? JsonObject ?: return null
        val up = obj.numberOrNull("totalBytesIn") ?: return null
        val down = obj.numberOrNull("totalBytesOut") ?: return null
        return up to down    }

    /**
     * 选择节点。
     *
     * 参数里的 [group] **不发给守护进程**：当前守护进程的 `proxies.select`
     * 只接受 `node`，且对不存在的节点会明确报错。传一个它不认识的 `group`
     * 字段会被忽略 —— 与其制造"传了但没用到"的错觉，不如在这里去掉，
     * 等策略组（M4 的 `ProxyGroup`）真正落到守护进程侧再一起加。
     */
    override suspend fun select(group: String, node: String): SyfuseResult<Unit> =
        when (
            val result = client.call(
                "proxies.select",
                buildJsonObject { put("node", node) },
            )
        ) {
            is SyfuseResult.Success -> SyfuseResult.Success(Unit)
            is SyfuseResult.Failure -> result
        }

    /**
     * 测速。
     *
     * 守护进程侧（T2）做的是 **TCP 可达性预检**：对每个节点的 server:port
     * 发起一次带超时的连接，成功即报实测毫秒数；失败报 -1 + 原因。
     *
     * 返回值语义：`Map<节点名, 延迟毫秒>`；**值为 null 表示该节点测速失败**
     * （守护进程给了 -1），而不是"尚未测速" —— 后者用 key 不存在表示。
     * 这样界面能把"没测过"与"测了但连不上"分开呈现。
     */
    override suspend fun urlTest(names: List<String>): SyfuseResult<Map<String, Int?>> {
        val result = client.call(
            "proxies.urlTest",
            buildJsonObject {
                put("nodes", JsonArray(names.map { JsonPrimitive(it) }))
            },
        )
        return when (result) {
            is SyfuseResult.Success -> {
                val obj = result.value as? JsonObject
                val arr = obj?.get("results") as? JsonArray
                if (arr == null) {
                    SyfuseResult.Success(emptyMap())
                } else {
                    val out = LinkedHashMap<String, Int?>()
                    for (el in arr) {
                        val row = el as? JsonObject ?: continue
                        val name = (row["node"] as? JsonPrimitive)?.contentOrNull ?: continue
                        val delay = (row["delayMs"] as? JsonPrimitive)?.contentOrNull?.toIntOrNull()
                        // -1 是守护进程的"失败"约定，对外表示成 null。
                        out[name] = delay?.takeIf { it >= 0 }
                    }
                    SyfuseResult.Success(out)
                }
            }
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

    // ---------------------------------------------------------------- 内部工具函数

    private fun JsonObject.stringOrNull(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull
    private fun JsonObject.numberOrNull(key: String) = (this[key] as? JsonPrimitive)?.long
    private fun JsonObject.boolOrNull(key: String) = (this[key] as? JsonPrimitive)?.boolean
    private fun String.toEpochMillis(): Long = runCatching { java.time.Instant.parse(this).toEpochMilli() }.getOrNull() ?: -1L
    private fun labelsOf(rec: JsonObject): Map<String, String> {
        val raw = rec["labels"] as? JsonObject ?: return emptyMap()
        val out = mutableMapOf<String, String>()
        for (k in raw.keys) {
            val v = raw[k] as? JsonPrimitive
            if (v != null && v.isString) out[k] = v.content
        }
        return out
    }
}
