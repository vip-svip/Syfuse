package io.github.vipsvip.syfuse.daemon

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.util.concurrent.atomic.AtomicLong

/**
 * 守护进程客户端。
 *
 * 传输交给 [DaemonTransport]。**刻意不给默认值** —— 默认值只能取
 * `android.net.LocalSocket` 那个真机实现，而它住在 `:daemon`（Android library）里，
 * 本模块是纯 JVM，引用不到它。这个"引用不到"是**好事**：
 * 它从类型系统上保证了本模块（连同它的单测）永远不碰 Android 框架类，
 * 于是"协议逻辑 + 真实守护进程"这条往返可以在**无设备**的沙箱里被真实跑通
 * （见 `src/test/.../DaemonWireRoundTripTest.kt`）。
 * 真机装配点在 `:engine` 的 `EngineModule`，那里显式传入 `LocalSocketTransport()`。
 *
 * 协议是"一行一个 JSON"（见 [DaemonProtocol]），因此实现上就是
 * 写一行、读一行，不需要处理粘包/半包之外的复杂情况。
 *
 * 连接策略：**每次调用新建连接**，不维护长连接。
 * 理由：控制通道的调用频率极低（用户点一下才有一次），
 * 长连接会带来"连接悄悄断了但 App 以为还在"的经典问题；
 * 而 UDS 建连成本是微秒级，不值得为省这点开销引入状态管理。
 * 后续需要 `WS /traffic` 这类推送时，再单独做一条订阅通道。
 */
class DaemonClient(
    private val dataDir: String = DaemonProtocol.DATA_DIR,
    private val tokenProvider: () -> String?,
    private val transport: DaemonTransport,
) {
    private val seq = AtomicLong(0)

    val socketPath: String get() = "$dataDir/${DaemonProtocol.SOCKET_NAME}"
    val tokenPath: String get() = "$dataDir/${DaemonProtocol.TOKEN_NAME}"
    val pidPath: String get() = "$dataDir/${DaemonProtocol.PID_NAME}"

    /**
     * 调用一个方法。
     *
     * 返回 [SyfuseResult]，把"未接入 / 未授权 / 版本不匹配"等情形
     * 表达成**结构化错误**而不是抛异常 —— 这些都不是异常状况，
     * 而是 UI 需要分别呈现的正常状态。
     */
    suspend fun call(method: String, params: JsonElement? = null): SyfuseResult<JsonElement> =
        withContext(Dispatchers.IO) {
            val token = tokenProvider()
                ?: return@withContext SyfuseResult.Failure(
                    SyfuseError(
                        code = ErrorCode.MODULE_NOT_INSTALLED,
                        technical = "no_token",
                        userMessage = "尚未获取访问令牌",
                        hint = "令牌由模块首次启动时生成，见 $tokenPath；也可在模块的 action.sh 第 9 项查看",
                    ),
                )

            val request = DaemonRequest(
                id = seq.incrementAndGet().toString(),
                authorization = token,
                method = method,
                params = params,
            )

            try {
                val line = transport.exchange(
                    socketPath,
                    DaemonProtocol.json.encodeToString(DaemonRequest.serializer(), request),
                    DaemonProtocol.TIMEOUT_MS.toInt(),
                )
                if (line == null) {
                    return@withContext SyfuseResult.Failure(
                        SyfuseError(
                            code = ErrorCode.ENGINE_IPC_FAILED,
                            technical = "empty_response",
                            userMessage = "守护进程没有返回内容",
                            hint = "进程可能已退出。可在模块的 action.sh 第 3 项查看状态",
                        ),
                    )
                }
                decode(line)
            } catch (t: Throwable) {
                // 连不上是**最常见**的情形（模块未安装 / 守护未启动），
                // 必须给出可操作的提示，而不是把 SocketException 原文抛给用户。
                SyfuseResult.Failure(
                    SyfuseError(
                        code = ErrorCode.ENGINE_IPC_FAILED,
                        technical = "connect_failed: ${t::class.qualifiedName}: ${t.message}",
                        userMessage = "无法连接守护进程",
                        hint = "可能原因：模块未安装、守护未启动、或设备无 root。" +
                            "无 root 时 App 会走内置内核（不经过本通道）。",
                        cause = t,
                    ),
                )
            }
        }

    private fun decode(line: String): SyfuseResult<JsonElement> {
        val resp = try {
            DaemonProtocol.json.decodeFromString(DaemonResponse.serializer(), line)
        } catch (t: Throwable) {
            return SyfuseResult.Failure(
                SyfuseError(
                    code = ErrorCode.ENGINE_IPC_FAILED,
                    technical = "${DaemonErrorCode.ENCODE_ERROR}: ${t.message}",
                    userMessage = "守护进程返回了无法解析的内容",
                    hint = "App 与模块版本可能不匹配",
                    cause = t,
                ),
            )
        }
        if (resp.ok) {
            val result = resp.result
            return if (result == null) {
                SyfuseResult.Failure(
                    SyfuseError(
                        code = ErrorCode.ENGINE_IPC_FAILED,
                        technical = "empty_result",
                        userMessage = "守护进程返回成功但没有结果（缺陷，请反馈）",
                    ),
                )
            } else {
                SyfuseResult.Success(result)
            }
        }
        return SyfuseResult.Failure(
            SyfuseError(
                code = if (resp.code == DaemonErrorCode.UNAUTHORIZED) {
                    ErrorCode.MODULE_NOT_INSTALLED
                } else {
                    ErrorCode.ENGINE_IPC_FAILED
                },
                technical = "${resp.code ?: "unknown"}: ${resp.error ?: ""}",
                userMessage = daemonErrorHint(resp.code, resp.error),
                hint = resp.error,
            ),
        )
    }

    /** 便捷方法：查询守护进程状态。 */
    suspend fun status(): SyfuseResult<JsonElement> = call("status")

    /** 便捷方法：查询版本与能力。 */
    suspend fun version(): SyfuseResult<JsonElement> = call("version")

    /** 便捷方法：还原清单摘要（含逐条校验问题）。 */
    suspend fun manifestSummary(): SyfuseResult<JsonElement> = call("manifest.summary")

    /** 便捷方法：读取最近日志。 */
    suspend fun tailLog(lines: Int = 100): SyfuseResult<JsonElement> =
        call("log.tail", DaemonProtocol.json.parseToJsonElement("""{"lines":$lines}"""))

    /** 便捷方法：规则集统计（真实编译一次后报数）。 */
    suspend fun rulesStats(): SyfuseResult<JsonElement> = call("rules.stats")

    /** 便捷方法：规则体检（带行号的逐条诊断）。 */
    suspend fun rulesSyntax(): SyfuseResult<JsonElement> = call("rules.syntax")

    /**
     * 便捷方法：对单个请求跑一次匹配。
     *
     * 与"规则测试"界面一一对应：传入 host 或 url，拿回决策、优先级与
     * 完整候选链路（哪条规则命中、哪条被跳过、为什么）。
     *
     * @param collectChain 为 true 时守护进程会返回完整链路；界面展示需要它，
     *   高频调用不需要（链路对象是纯展示用的额外分配）。
     */
    suspend fun rulesTest(
        host: String? = null,
        url: String? = null,
        resourceType: String? = null,
        documentHost: String? = null,
        collectChain: Boolean = false,
    ): SyfuseResult<JsonElement> = call(
        "rules.test",
        buildJsonObject {
            if (host != null) put("host", host)
            if (url != null) put("url", url)
            if (resourceType != null) put("resourceType", resourceType)
            if (documentHost != null) put("documentHost", documentHost)
            put("collectChain", collectChain)
        },
    )
}

/**
 * 方法可用性判定。
 *
 * UI 必须用它来决定入口是"可用"还是"实验性/不可用" ——
 * 这是提示词 12.2「禁止死 UI」在代码层的执行点：
 * 界面不能只凭"有这个按钮"就认为功能存在。
 */
object DaemonCapability {
    fun isImplemented(method: String): Boolean = method in DaemonProtocol.IMPLEMENTED_METHODS

    /** 返回计划里程碑；已实现则返回 null。 */
    fun pendingMilestone(method: String): String? = DaemonProtocol.PENDING_METHODS[method]
}
