package io.github.vipsvip.syfuse.domain.engine

import io.github.vipsvip.syfuse.core.rule.CompiledNetworkRule
import io.github.vipsvip.syfuse.core.rule.Decision
import io.github.vipsvip.syfuse.core.rule.FilterEngine
import io.github.vipsvip.syfuse.core.rule.MatchRequest
import io.github.vipsvip.syfuse.core.rule.Priority
import io.github.vipsvip.syfuse.core.rule.ResourceType

/**
 * 数据面路由决策（M5）：把"一次连接应该走哪条出站"从规则引擎的判定推导出来。
 *
 * 在这之前，这条决策在 Kotlin 侧**不存在**：`core:rule` 的 FilterEngine 只回答
 * "这条请求是被拦截、放行还是重写"（广告过滤语义），`DataPlane.select` 只负责
 * 用户手动选节点，两者之间没有任何一层把规则结果翻译成出站动作。本类就是那一层。
 *
 * 引擎决策 → 出站动作的映射表（本类唯一的语义决策点，逐条给理由）：
 *
 * | 引擎决策  | 出站动作               | 理由 |
 * | --------- | --------------------- | ---- |
 * | BLOCK     | REJECT                | 命中拦截类规则（域名/URL/通配/正则/hosts 黑洞/dnsmasq `address=/d/`），连接不得建立 |
 * | ALLOW     | DIRECT                | 命中例外放行（`@@`）规则：明确放行即直连，不再落入默认动作 |
 * | REWRITE   | DIRECT（+ [RouteDecision.rewrite]） | 重写的载荷是**本地伪造应答**，连接根本不出网；出站选择上等同直连，载荷交由 DNS/HTTP 层消费 |
 * | NONE      | [RoutingPolicy.noMatch] | 未命中任何规则。默认动作**必须显式配置**，不允许"没配置就当直连" |
 *
 * 两个必须讲清楚的边界：
 *
 *  1. **PROXY 今天没有规则级生产者。** `RuleKind.ROUTE` 在枚举里声明了，但
 *     RuleParser 从不产出它（路由规则的语法尚未定义）。因此 PROXY 只能经
 *     默认动作进入 —— 这与 mihomo 的 `MATCH` 尾规则语义一致。本类不为此
 *     编造语法，也不把 ALLOW 偷偷升级成 PROXY。
 *  2. **REWRITE 不是 REJECT。** `$redirect` / `$empty` 在语义上是"用伪造内容
 *      neutralize 这个请求"，确实不出网；但它与"拒绝建连"对用户是两件事
 *     （前者页面还在、只是内容被替换）。所以出站记为 DIRECT，用
 *     [RouteDecision.engineDecision] = REWRITE 让上层能区分两者。
 *
 * 本类是**纯函数**：不依赖 Android framework、不发起 IO、可整体 JVM 单测。
 * 真正的接线（在 `engine` 模块的 UdsDataPlane 或守护进程侧调用它）不在本单元范围。
 */
class RuleRouter(
    private val engine: FilterEngine,
    private val policy: RoutingPolicy = RoutingPolicy(),
) {

    /**
     * 对一次连接做出站决策。
     *
     * 热路径约束：不收集匹配链路（collectChain = false），不为每次匹配分配链路对象。
     */
    fun decide(target: RoutingTarget): RouteDecision {
        val result = engine.match(target.toMatchRequest())
        return when (result.decision) {
            Decision.BLOCK -> RouteDecision(
                action = RouteAction.REJECT,
                node = null,
                engineDecision = Decision.BLOCK,
                enginePriority = result.priority,
                matchedRule = result.matchedRule,
                rewrite = null,
                reason = ruleReason("命中拦截规则", result.matchedRule, result.priority),
            )

            Decision.ALLOW -> RouteDecision(
                action = RouteAction.DIRECT,
                node = null,
                engineDecision = Decision.ALLOW,
                enginePriority = result.priority,
                matchedRule = result.matchedRule,
                rewrite = null,
                reason = ruleReason("命中放行规则（@@ 例外）", result.matchedRule, result.priority),
            )

            Decision.REWRITE -> RouteDecision(
                action = RouteAction.DIRECT,
                node = null,
                engineDecision = Decision.REWRITE,
                enginePriority = result.priority,
                matchedRule = result.matchedRule,
                rewrite = result.matchedRule?.rewritePayload(),
                reason = ruleReason("命中重写规则（本地伪造应答，不出网）", result.matchedRule, result.priority),
            )

            Decision.NONE -> noMatchDecision()
        }
    }

    // ---------------------------------------------------------------- 内部

    /**
     * 把引擎判出的 REWRITE 规则翻译成具体载荷。
     *
     * 顺序必须与 `CompiledNetworkRule.action` 的判定顺序**一致**
     * （dnsRewrite → redirect → empty）：那边第一个非 null 的条件决定了
     * decision 是 REWRITE，这边必须挑同一个来源，否则会出现"引擎说是重写、
     * 映射层却给了另一种载荷"的错配。
     *
     * 返回 null 只在引擎契约被破坏时发生（判了 REWRITE 却三个来源都空），
     * 此时**宁可如实留空**，也不编造一个空载荷冒充成功。
     */
    private fun CompiledNetworkRule.rewritePayload(): RewritePayload? {
        mods.dnsRewrite?.let { return RewritePayload.Dns(it) }
        mods.redirect?.let { return RewritePayload.Redirect(it) }
        if (mods.empty) return RewritePayload.Empty
        return null
    }

    private fun RoutingTarget.toMatchRequest(): MatchRequest = MatchRequest(
        host = host,
        url = url,
        resourceType = resourceType ?: ResourceType.OTHER,
        documentHost = documentHost,
        client = client,
        ctag = ctag,
        method = method,
    )

    /**
     * 未命中任何规则时的决策。这是"默认动作必须显式"的落点：
     * 无论策略选什么，理由串都要写明"未命中任何规则"，让用户与日志都能
     * 区分"规则放行的直连"和"没规则管它的直连"。
     */
    private fun noMatchDecision(): RouteDecision = when (val noMatch = policy.noMatch) {
        DefaultAction.DIRECT -> RouteDecision(
            action = RouteAction.DIRECT,
            node = null,
            engineDecision = Decision.NONE,
            enginePriority = Priority.NONE,
            matchedRule = null,
            rewrite = null,
            reason = "未命中任何规则；按策略默认动作直连",
        )

        is DefaultAction.PROXY -> {
            // 空白节点名与 null 等价：都是"没有选中节点"。此时**不得**静默改判直连 ——
            // 那正是"没匹配就直连却不告诉用户"要防的事。action 保持 PROXY、
            // node 为 null，由调用方如实呈现（节点页引导或明确报错）。
            val node = noMatch.node?.takeIf { it.isNotBlank() }
            RouteDecision(
                action = RouteAction.PROXY,
                node = node,
                engineDecision = Decision.NONE,
                enginePriority = Priority.NONE,
                matchedRule = null,
                rewrite = null,
                reason = if (node == null) {
                    "未命中任何规则；策略默认动作为走代理，但尚未选择节点 —— " +
                        "请先在节点页选择节点，或把默认动作改为直连"
                } else {
                    "未命中任何规则；按策略默认动作走代理（节点 $node）"
                },
            )
        }
    }

    /**
     * 命中规则时的理由串。带行号与优先级名（用 expect.tsv / UDS JSON 同一套
     * 小写词表），供「规则测试」展示与连接日志直接使用。
     *
     * [rule] 对 BLOCK/ALLOW/REWRITE 必非空（FilterEngine 契约：非 NONE 决策
     * 一定带命中规则）。万一日后契约被破坏，宁可让理由写出"契约相悖"，
     * 也不要装作是正常的规则命中。
     */
    private fun ruleReason(prefix: String, rule: CompiledNetworkRule?, priority: Priority): String {
        if (rule == null) return "$prefix，但引擎未提供命中规则（FilterEngine 契约相悖）"
        return "$prefix（第 ${rule.lineNo} 行，${priorityName(priority)}）：${rule.raw}"
    }

    private fun priorityName(priority: Priority): String = when (priority) {
        Priority.IMPORTANT_ALLOW -> "important-allow"
        Priority.IMPORTANT_BLOCK -> "important-block"
        Priority.NORMAL_ALLOW -> "normal-allow"
        Priority.NORMAL_BLOCK -> "normal-block"
        Priority.NONE -> "none"
    }
}

/**
 * 一次连接的描述：[RuleRouter] 的决策输入。
 *
 * 字段取舍（当前能拿到什么就先用什么，拿不到的留 TODO 并说明）：
 *  - [host]：连接目标主机名或 IP 字面量。必填 —— 上层解析不出目标时应如实报错，
 *    而不是造一个空 host 来问规则。
 *  - [port]：目标端口，0 表示未知。**当前不参与匹配**：6.1 的规则语法还没有端口
 *    修饰符。字段先行落地并显式标注，避免以后做端口分流时误以为"一直都有"。
 *  - [url]：完整 URL。只有 HTTP 层/MITM 拿得到；连接层为 null，正则与 URL 锚定
 *    规则因此退化为只按 host 匹配。TODO(M8)：MITM 接入后回填。
 *  - [documentHost]：发起连接的来源页面域名。HTTP 层才有；连接层为 null。
 *  - [client]：连接归属的应用/客户端标识（对应 `$client` 作用域）。
 *    TODO(M9)：连接列表与按应用分流接入后回填，当前恒为 null。
 *  - [ctag]：客户端标签（对应 `$ctag` 作用域）。TODO(M10)：标签组接入后回填。
 *  - [resourceType]：资源类型。连接层拿不到，null 表示"未知"（按 `other` 匹配）。
 *  - [method]：HTTP 请求方法（对应 `$method` 作用域）。**连接层不可知**，故默认
 *    null；此时 `$method` 规则不会命中（引擎语义：要求方法而请求无方法 = 不匹配）。
 *    有 HTTP 语义的层（M8 MITM / 规则测试）**必须**回填，否则 `$method` 规则
 *    形同不存在。
 *
 *    为什么以前这里没有这个字段：早期实现假定"连接层拿不到方法"，于是整个
 *    Kotlin 侧都无法表达 `$method`。但 `tests/expect.tsv` 已启用 method 断言
 *    （189 行中含 `$method=POST` / `$method=~GET` 等 5 行），Go 侧
 *    `src/syfuse/rule/corpus_test.go` 一直把方法传进引擎 —— 于是两边语义漂移：
 *    同一行语料 Go 判 block、Kotlin 判 none。补齐字段是让混合预言机重新对齐的
 *    唯一办法。
 */
data class RoutingTarget(
    val host: String,
    val port: Int = 0,
    val url: String? = null,
    val documentHost: String? = null,
    val client: String? = null,
    val ctag: String? = null,
    val resourceType: ResourceType? = null,
    val method: String? = null,
) {
    init {
        require(host.isNotBlank()) {
            "连接目标不能为空 host —— 上层解析不出目标时应如实报错，而不是造一个空连接来问规则"
        }
    }
}

/** 出站动作。数据面按它选择直连 / 代理 / 拒绝。 */
enum class RouteAction {
    DIRECT,
    PROXY,
    REJECT,
}

/**
 * 未命中任何规则时的默认动作。**必须是显式选择**：
 * [RoutingPolicy] 没有"未配置"状态，构造它就等于表态。
 */
sealed interface DefaultAction {
    /** 默认直连。 */
    data object DIRECT : DefaultAction

    /**
     * 默认走代理。
     *
     * [node] 为当前选中节点名；null / 空白表示用户尚未选择节点。此时决策保持
     * PROXY 且 node=null（见 [RuleRouter] 的 noMatchDecision），调用方必须如实
     * 呈现，不得静默改判直连。
     */
    data class PROXY(val node: String?) : DefaultAction
}

/** 路由策略。当前只有"未命中时的默认动作"一项 —— 只放已经能兑现的配置，不放空头支票。 */
data class RoutingPolicy(
    val noMatch: DefaultAction = DefaultAction.DIRECT,
)

/**
 * 一次路由决策。
 *
 * [engineDecision] / [enginePriority] / [matchedRule] 原样透传引擎结果，
 * 让上层既能只看出站动作，也能在「规则测试」与连接日志里展示完整依据。
 * [reason] 是人类可读的一句话（含命中行号），任何分支都非空。
 */
data class RouteDecision(
    val action: RouteAction,
    /** 仅 [RouteAction.PROXY] 有意义；为 null 表示默认动作为代理但尚未选择节点。 */
    val node: String?,
    val engineDecision: Decision,
    val enginePriority: Priority,
    val matchedRule: CompiledNetworkRule?,
    /**
     * 仅 [Decision.REWRITE] 时非空：本次重写要伪造的应答内容。
     *
     * 为什么不能只留一个 `dnsRewrite: String?`：引擎的 REWRITE 有三个来源 ——
     * `$dnsrewrite`/hosts/dnsmasq（DNS 层改写地址）、`$redirect`（HTTP 层改写到
     * 本地资源）、`$empty`（返回空应答）。原实现只把第一种投影出来，于是
     * `$redirect` 与 `$empty` 规则**判为重写却没有载荷可消费** —— 上层拿到一个
     * DIRECT 决策，既不知道要跳转到哪，也不知道该返回空。那正是"壳"：
     * 决策做出来了，动作落不了地。
     */
    val rewrite: RewritePayload?,
    val reason: String,
) {
    /**
     * DNS 解析重写地址。仅当 [rewrite] 是 [RewritePayload.Dns] 时非空。
     * 保留为派生属性而非独立字段，避免"载荷"有两个可能不一致的副本。
     */
    val dnsRewrite: String?
        get() = (rewrite as? RewritePayload.Dns)?.address
}

/**
 * REWRITE 决策要伪造的应答内容。三种来源互斥 —— 引擎按
 * `dnsRewrite` / `redirect` / `empty` 的顺序择一（见 `CompiledNetworkRule.action`）。
 */
sealed interface RewritePayload {
    /** DNS 层：把该域名解析到 [address]（IP 字面量或 `local` 等 dnsmasq 关键字）。 */
    data class Dns(val address: String) : RewritePayload

    /** HTTP 层：把请求重写为本地资源 [target]（`$redirect=` 的值）。 */
    data class Redirect(val target: String) : RewritePayload

    /** HTTP 层：返回空应答（`$empty`），既不跳转也不出网。 */
    data object Empty : RewritePayload
}
