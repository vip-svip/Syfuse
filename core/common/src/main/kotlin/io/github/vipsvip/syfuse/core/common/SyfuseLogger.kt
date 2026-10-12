package io.github.vipsvip.syfuse.core.common

/**
 * 日志门面。
 *
 * R3（零遥测）的落地方式之一：日志只写本地环形缓冲与可导出的文本，
 * **没有任何上报出口**。实现方若尝试把日志发往网络，需要显式引入新的依赖，
 * 在评审中可见——而不是藏在一个 log() 调用背后。
 */
interface SyfuseLogger {
    fun debug(tag: String, message: String)
    fun info(tag: String, message: String)
    fun warn(tag: String, message: String, cause: Throwable? = null)
    fun error(tag: String, message: String, cause: Throwable? = null)
}

/** 分级枚举，与 5.7 的日志分级一致。 */
enum class LogLevel { DEBUG, INFO, WARN, ERROR }

/** 一条日志记录。 */
data class LogRecord(
    val timestampMs: Long,
    val level: LogLevel,
    val tag: String,
    val message: String,
    val throwable: Throwable? = null,
) {
    /** 导出为文本行。异常栈只写类型与消息，不写完整栈以免导出文件被路径信息污染。 */
    fun toLine(): String = buildString {
        append(timestampMs)
        append('\t')
        append(level.name)
        append('\t')
        append(tag)
        append('\t')
        append(message)
        throwable?.let {
            append(" | ")
            append(it::class.qualifiedName)
            append(": ")
            append(it.message)
        }
    }
}
