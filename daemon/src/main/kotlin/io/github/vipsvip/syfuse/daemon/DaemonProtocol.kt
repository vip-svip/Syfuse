package io.github.vipsvip.syfuse.daemon

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement

/**
 * 守护进程通信协议。
 *
 * **本文件与 `src/internal/ipc/ipc.go` 是同一份协议的两侧实现，必须同步修改。**
 * 任何一侧的字段名/语义变化都要同时改另一侧，并更新 [PROTOCOL_VERSION]。
 *
 * 传输：Unix domain socket（默认 `/data/adb/syfuse/control.sock`）
 * 编码：换行分隔的 JSON（一行一个请求 / 一行一个应答）
 *
 * 关于 protobuf（诚实说明）：
 * 提示词 3.4 写的是 "UDS + protobuf + token"。Go 侧目前用 JSON，
 * 理由是保持零第三方依赖（见 src/go.mod）。协议本身已按
 * "版本号 + 方法名 + 结构化参数"设计，换编码只需改本文件的序列化，
 * 方法语义不变。这一点在 README 的偏离登记里也写了。
 */
object DaemonProtocol {
    /** 协议版本。App 与模块可独立升级，靠它协商。 */
    const val PROTOCOL_VERSION = 1

    /** socket 文件名。 */
    const val SOCKET_NAME = "control.sock"

    /** 令牌文件名。 */
    const val TOKEN_NAME = "api.token"

    /** 守护进程 PID 文件名。 */
    const val PID_NAME = "syfused.pid"

    /** 还原清单相对路径。 */
    const val MANIFEST_REL_PATH = "manifest/installed.json"

    /** 数据根目录（root 形态下的固定位置）。 */
    const val DATA_DIR = "/data/adb/syfuse"

    /** 单次请求的超时（毫秒）。UDS 是本机通信，超过这个时间一定是异常。 */
    const val TIMEOUT_MS = 5_000L

    /** JSON 配置：忽略未知字段，保证 App 比模块新时不会解析失败（前向兼容）。 */
    val json: Json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
    }

    /** 已实现的方法名。未列出的方法调用会拿到明确的"未实现"错误。 */
    val IMPLEMENTED_METHODS = setOf(
        "status", "version", "manifest.summary", "token.rotate", "log.tail",
    )

    /** 已注册但**尚未实现**的方法 → 计划里程碑。UI 据此把入口标为"实验性/不可用"。 */
    val PENDING_METHODS: Map<String, String> = mapOf(
        "proxies" to "M3/M4",
        "connections" to "M9",
        "traffic" to "M9",
        "rules.test" to "M5/M10",
        "filter.stats" to "M10",
        "hook.status" to "M11",
        "leak.report" to "M12",
        "leak.fix" to "M12",
        "config.reload" to "M5",
        "proxies.select" to "M4",
    )
}

/** 一次调用。字段名与 Go 侧 `ipc.Request` 一一对应。 */
@Serializable
data class DaemonRequest(
    @SerialName("protocolVersion") val protocolVersion: Int = DaemonProtocol.PROTOCOL_VERSION,
    @SerialName("id") val id: String,
    @SerialName("authorization") val authorization: String,
    @SerialName("method") val method: String,
    @SerialName("params") val params: JsonElement? = null,
)

/** 一次应答。字段名与 Go 侧 `ipc.Response` 一一对应。 */
@Serializable
data class DaemonResponse(
    @SerialName("id") val id: String = "",
    @SerialName("ok") val ok: Boolean = false,
    @SerialName("error") val error: String? = null,
    @SerialName("code") val code: String? = null,
    @SerialName("result") val result: JsonElement? = null,
)

/** 错误码。与 Go 侧 dispatch 里写出的 code 保持一致。 */
object DaemonErrorCode {
    const val BAD_REQUEST = "bad_request"
    const val UNAUTHORIZED = "unauthorized"
    const val VERSION_TOO_NEW = "version_too_new"
    const val UNKNOWN_METHOD = "unknown_method"
    const val HANDLER_ERROR = "handler_error"
    const val ENCODE_ERROR = "encode_error"
}

/**
 * 把守护进程返回的错误码翻译成用户能懂的话。
 *
 * 这一步不能省：`unauthorized` 对用户毫无意义，而
 * "访问令牌不匹配，请重新获取" 他能立刻知道该做什么。
 */
fun daemonErrorHint(code: String?, raw: String?): String = when (code) {
    DaemonErrorCode.UNAUTHORIZED -> "访问令牌不匹配。请在「设置 → 模块与特权」重新获取令牌。"
    DaemonErrorCode.VERSION_TOO_NEW -> "模块比 App 新，请升级 App。"
    DaemonErrorCode.UNKNOWN_METHOD -> "模块不认识这个请求，可能是版本不匹配。"
    DaemonErrorCode.BAD_REQUEST -> "请求格式错误（这是缺陷，请反馈）。"
    else -> raw ?: "未知错误"
}
