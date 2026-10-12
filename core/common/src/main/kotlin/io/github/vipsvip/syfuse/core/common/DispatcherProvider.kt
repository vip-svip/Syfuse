package io.github.vipsvip.syfuse.core.common

import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers

/**
 * 调度器抽象。
 *
 * 目的：让领域层与数据层不直接引用 [Dispatchers]，从而可以在 JVM 单测里
 * 注入测试调度器。这是本工程把尽可能多的逻辑放进"可单测"范围的基础设施之一。
 */
interface DispatcherProvider {
    val main: CoroutineDispatcher
    val io: CoroutineDispatcher
    val default: CoroutineDispatcher
}

/** 生产实现。 */
class DefaultDispatcherProvider : DispatcherProvider {
    override val main: CoroutineDispatcher get() = Dispatchers.Main
    override val io: CoroutineDispatcher get() = Dispatchers.IO
    override val default: CoroutineDispatcher get() = Dispatchers.Default
}
