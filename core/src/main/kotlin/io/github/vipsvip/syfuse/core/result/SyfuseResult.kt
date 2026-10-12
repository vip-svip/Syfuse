package io.github.vipsvip.syfuse.core.result

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

/**
 * 统一的失败模型。
 *
 * 设计取舍：不用异常传递可预期的业务失败（如订阅 404、规则语法错误），因为异常会
 * 丢失「错误位置」这类结构化信息，而提示词 5.3 要求「非法文件给出精确错误位置而非崩溃」。
 * 异常仅用于真正的编程错误。
 */
sealed interface SyfuseError {
    /** 人可读的简短说明，用于 UI 主文案。 */
    val message: String

    /** 订阅或远程规则集拉取失败。 */
    data class Network(val url: String, override val message: String, val cause: Throwable? = null) : SyfuseError

    /** 配置文件解析失败，[line] / [column] 用于把错误定位到行列（提示词 8.1）。 */
    data class Parse(
        val source: String,
        override val message: String,
        val line: Int? = null,
        val column: Int? = null,
    ) : SyfuseError

    /** 校验失败，例如 schema 版本不兼容或必填字段缺失。 */
    data class Validation(val field: String, override val message: String) : SyfuseError

    /** 内核启动或重载失败。 */
    data class Engine(override val message: String, val exitCode: Int? = null) : SyfuseError

    /** 权限不足，例如无 root、未授予 VPN 授权。 */
    data class Permission(val capability: String, override val message: String) : SyfuseError

    /** 尚未实现的能力。M1 阶段大量使用，属预期状态而非缺陷。 */
    data class NotImplemented(val feature: String) : SyfuseError {
        override val message: String get() = "功能尚未实现：$feature"
    }
}

/** 承载成功值或 [SyfuseError] 的结果类型。 */
sealed interface SyfuseResult<out T> {
    data class Success<T>(val value: T) : SyfuseResult<T>
    data class Failure(val error: SyfuseError) : SyfuseResult<Nothing>

    val isSuccess: Boolean get() = this is Success
}

/** 取值，失败时返回 null。 */
fun <T> SyfuseResult<T>.getOrNull(): T? = (this as? SyfuseResult.Success)?.value

/** 取值，失败时抛异常（仅用于确实不可失败的场景，如已校验过的内部调用）。 */
fun <T> SyfuseResult<T>.getOrThrow(): T = when (this) {
    is SyfuseResult.Success -> value
    is SyfuseResult.Failure -> error("预期成功但得到失败：${error.message}")
}

/** 映射成功值，失败原样透传。 */
inline fun <T, R> SyfuseResult<T>.map(transform: (T) -> R): SyfuseResult<R> = when (this) {
    is SyfuseResult.Success -> SyfuseResult.Success(transform(value))
    is SyfuseResult.Failure -> this
}

/** 把结果流映射为值流，失败时发射 null，便于 UI 做四态渲染。 */
fun <T, R> Flow<SyfuseResult<T>>.mapValue(transform: (T) -> R): Flow<R?> = map { it.map(transform).getOrNull() }
