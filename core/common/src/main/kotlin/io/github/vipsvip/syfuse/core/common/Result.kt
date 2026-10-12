package io.github.vipsvip.syfuse.core.common

/**
 * 统一的结果类型。
 *
 * 设计取舍：不使用异常做流程控制。规则解析、订阅拉取、配置导入这类操作
 * 的失败是**预期内**的常态（非法配置、网络 404、哈希不匹配），
 * 用返回类型表达可以让调用方无法忽略失败分支。
 */
sealed interface SyfuseResult<out T> {
    data class Success<T>(val value: T) : SyfuseResult<T>

    data class Failure(val error: SyfuseError) : SyfuseResult<Nothing>

    fun getOrNull(): T? = (this as? Success)?.value

    fun errorOrNull(): SyfuseError? = (this as? Failure)?.error
}

/** 错误模型。每个错误都必须能回答"用户看到什么"与"技术上是什么"。 */
data class SyfuseError(
    val code: ErrorCode,
    val technical: String,
    /** 面向用户的中文说明，不允许为空——空字符串会逼迫 UI 显示技术细节。 */
    val userMessage: String,
    /** 可选的修复建议，对应 7.3 的"错误态含原因与重试"。 */
    val hint: String? = null,
    val cause: Throwable? = null,
)

/**
 * 错误码。
 * 分段的用意：UI 可以按段位决定呈现方式（例如 CONFIG_* 一律引导到配置页）。
 */
enum class ErrorCode {
    // 1xxx 规则与过滤
    RULE_PARSE_ERROR,
    RULE_COMPILE_ERROR,
    RULE_SET_HASH_MISMATCH,

    // 2xxx 配置与订阅
    CONFIG_INVALID,
    CONFIG_VERSION_UNSUPPORTED,
    SUBSCRIPTION_FETCH_FAILED,
    SUBSCRIPTION_FORMAT_UNKNOWN,

    // 3xxx 内核与隧道
    ENGINE_START_FAILED,
    ENGINE_IPC_FAILED,
    VPN_PERMISSION_DENIED,

    // 4xxx 特权
    ROOT_NOT_AVAILABLE,
    PRIVILEGE_COMMAND_REJECTED,
    MODULE_NOT_INSTALLED,

    // 5xxx 通用
    NOT_FOUND,
    IO_ERROR,
    CANCELLED,
    UNKNOWN,
}

inline fun <T, R> SyfuseResult<T>.map(transform: (T) -> R): SyfuseResult<R> = when (this) {
    is SyfuseResult.Success -> SyfuseResult.Success(transform(value))
    is SyfuseResult.Failure -> this
}

inline fun <T> SyfuseResult<T>.onSuccess(action: (T) -> Unit): SyfuseResult<T> {
    if (this is SyfuseResult.Success) action(value)
    return this
}

inline fun <T> SyfuseResult<T>.onFailure(action: (SyfuseError) -> Unit): SyfuseResult<T> {
    if (this is SyfuseResult.Failure) action(error)
    return this
}

fun <T> syfuseRun(block: () -> T): SyfuseResult<T> = try {
    SyfuseResult.Success(block())
} catch (t: Throwable) {
    SyfuseResult.Failure(
        SyfuseError(
            code = ErrorCode.UNKNOWN,
            technical = "${t::class.qualifiedName}: ${t.message}",
            userMessage = "操作失败",
            cause = t,
        ),
    )
}
