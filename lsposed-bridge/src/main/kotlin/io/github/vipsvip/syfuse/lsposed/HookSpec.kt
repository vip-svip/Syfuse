package io.github.vipsvip.syfuse.lsposed

/**
 * LSPosed 桥接层的 Hook 描述（提示词 6.6）。
 *
 * M1 只定义数据模型与启停契约，不含任何 Xposed API 调用 ——
 * 这样做的原因是：Xposed API 在编译期只是 provided 依赖，代码本身无法在
 * 普通设备上验证；先固定「每个 Hook 必须有什么」的契约，
 * 能让 M11 的实现不会漏掉提示词 6.6 要求的四项：
 * 作用域配置、独立开关、命中统计、异常兜底。
 */
data class HookSpec(
    /** Hook 的唯一标识，与 UI 开关一一对应。 */
    val id: String,

    /** 人类可读名称。 */
    val displayName: String,

    /** 生效的目标包名列表（空表示由用户在 LSPosed 管理器中配置）。 */
    val scopePackages: List<String> = emptyList(),

    /** 独立开关，默认关闭 —— 提示词 6.6 与 R6 都要求显式启用。 */
    val enabledByDefault: Boolean = false,

    /**
     * 异常兜底策略。
     *
     * 必须是显式的：Hook 抛异常会拖垮目标 App 的进程，
     * 因此每个 Hook 都必须声明失败时如何处理。
     */
    val failurePolicy: FailurePolicy = FailurePolicy.SILENT_FALLBACK,
)

/**
 * Hook 失败策略。
 *
 * 默认 [SILENT_FALLBACK] 而非 [LOG]：提示词 6.6 要求「失败静默回退」，
 * 因为 Hook 的失败不应影响被注入 App 的正常使用。
 */
enum class FailurePolicy {
    /** 静默回退到未 Hook 的原始行为，不打断目标 App。 */
    SILENT_FALLBACK,

    /** 记录日志后回退，用于调试阶段。 */
    LOG_AND_FALLBACK,
}

/**
 * Hook 注册表契约。
 *
 * M11 的实现类将实现此接口。M1 只提供接口与一个空实现，
 * 用于让 App 侧的能力探测能查询「LSPosed 桥接是否可用」。
 */
interface HookRegistry {
    /** 全部已知 Hook 描述。 */
    fun hooks(): List<HookSpec>

    /**
     * 查询某 Hook 是否实际处于激活状态。
     *
     * 与 [HookSpec.enabledByDefault] 的区别：开关是用户意图，激活是实际生效状态。
     * 两者可能不一致（例如模块未被 LSPosed 启用），UI 必须展示实际状态。
     */
    fun isActive(hookId: String): Boolean
}

/**
 * M1 的空实现。
 *
 * 明确返回「无 Hook」，而非假装激活 —— 依据是红线条 R4（不造假）
 * 与提示词 2.3 第 11 条（不假装已生效）。
 */
object NoOpHookRegistry : HookRegistry {
    override fun hooks(): List<HookSpec> = emptyList()
    override fun isActive(hookId: String): Boolean = false
}
