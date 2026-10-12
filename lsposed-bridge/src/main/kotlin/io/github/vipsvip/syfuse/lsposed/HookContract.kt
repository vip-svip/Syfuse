package io.github.vipsvip.syfuse.lsposed

/**
 * LSPosed 增强层的对外契约。
 *
 * 6.6 要求每个 Hook 都必须具备：作用域配置、独立开关、命中统计、异常兜底、失败静默回退。
 * 把契约定义在这里（而不是散在 Hook 实现里），使"有没有做到这五项"可以被静态检查：
 * [HookSpec] 必须带 scope 与 enabled，[HookStats] 必须被上报，注册中心必须在异常时回退。
 */
data class HookSpec(
    val id: String,
    val title: String,
    /** 生效的目标包名；空表示不注入任何应用。 */
    val scope: Set<String>,
    val enabled: Boolean = false,
    /** 该 Hook 的作用说明，会显示在设置页——危险能力必须显式告知（R6）。 */
    val riskDescription: String,
)

/** 命中统计。 */
data class HookStats(
    val hookId: String,
    val hitCount: Long = 0,
    val failureCount: Long = 0,
    val lastError: String? = null,
)

/** Hook 注册中心：统一承担异常兜底与统计。 */
object HookRegistry {

    private val stats = LinkedHashMap<String, HookStats>()

    @Synchronized
    fun recordHit(hookId: String) {
        stats[hookId] = (stats[hookId] ?: HookStats(hookId)).let {
            it.copy(hitCount = it.hitCount + 1)
        }
    }

    @Synchronized
    fun recordFailure(hookId: String, error: Throwable) {
        stats[hookId] = (stats[hookId] ?: HookStats(hookId)).let {
            it.copy(
                failureCount = it.failureCount + 1,
                lastError = "${error::class.simpleName}: ${error.message}",
            )
        }
    }

    @Synchronized
    fun snapshot(): List<HookStats> = stats.values.toList()

    /**
     * 统一的失败兜底。任何 Hook 内部异常都不得让目标应用崩溃——
     * 增强层是附加能力，绝不能因为自身缺陷破坏被注入应用的稳定性。
     */
    inline fun guarded(hookId: String, block: () -> Unit) {
        try {
            block()
        } catch (t: Throwable) {
            recordFailure(hookId, t)
        }
    }
}

/** 内置 Hook 定义。 */
object SyfuseHooks {

    /** 向指定应用注入用户 CA 信任，免改 APK 实现 MITM（6.6）。 */
    val TRUST_USER_CA = HookSpec(
        id = "trust_user_ca",
        title = "注入用户 CA 信任",
        scope = emptySet(),
        enabled = false,
        riskDescription = "使目标应用信任本机安装的用户 CA，从而允许 HTTPS 解密。" +
            "仅对你有权分析的应用启用；会使该应用失去对中间人攻击的原有防护。",
    )

    /** 指定应用的 DNS 或代理覆盖（6.6）。 */
    val PER_APP_DNS = HookSpec(
        id = "per_app_dns",
        title = "按应用 DNS 覆盖",
        scope = emptySet(),
        enabled = false,
        riskDescription = "覆盖目标应用的 DNS 解析结果，使其走指定上游。",
    )

    /** 网络策略绕过与按应用强制代理（6.6）。 */
    val FORCE_PROXY = HookSpec(
        id = "force_proxy",
        title = "按应用强制代理",
        scope = emptySet(),
        enabled = false,
        riskDescription = "强制目标应用的网络请求经过本机代理，即使该应用声明了网络策略例外。",
    )

    val all: List<HookSpec> = listOf(TRUST_USER_CA, PER_APP_DNS, FORCE_PROXY)
}
