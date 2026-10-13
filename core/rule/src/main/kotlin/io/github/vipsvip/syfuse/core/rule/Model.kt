package io.github.vipsvip.syfuse.core.rule

/**
 * 过滤规则引擎的数据模型。
 *
 * 本文件刻意不依赖任何 Android API，使规则语义可在最弱的工具链（仅 JDK）上被单测证明。
 * 语义来源：AdGuard Home 的规则语义 × uBlock Origin 的匹配与优先级模型（见 README 致谢段）。
 * 实现为独立重写，未复制任何第三方源码。
 */

/** 规则在引擎中的语义类别，与第 10 部分 RuleEntry.kind 对齐。 */
enum class RuleKind {
    FILTER,
    ROUTE,
    COSMETIC,
    HOSTS,
    BLOCK,
}

/** 匹配命中后对请求施加的动作。 */
enum class Decision {
    /** 拦截。 */
    BLOCK,

    /** 放行（例外规则命中）。 */
    ALLOW,

    /** 重写（$dnsrewrite / $redirect / $empty 等）。 */
    REWRITE,

    /** 未命中任何规则，按默认策略放行。 */
    NONE,
}

/**
 * 优先级层级，rank 越大越优先。
 *
 * 判定顺序严格为 6.1 规定的：
 * `重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中（默认放行）`。
 * 引擎不采用"先命中先返回"的短路策略，而是收集全部候选后按 rank 取最大者，
 * 否则 `$important` 与 `@@` 的语义无法被正确表达。
 */
enum class Priority(val rank: Int) {
    IMPORTANT_ALLOW(4),
    IMPORTANT_BLOCK(3),
    NORMAL_ALLOW(2),
    NORMAL_BLOCK(1),
    NONE(0),
}

/** 资源类型，对应 6.1 的资源类型修饰符。 */
enum class ResourceType(val token: String) {
    DOCUMENT("document"),
    SUBDOCUMENT("subdocument"),
    SCRIPT("script"),
    IMAGE("image"),
    STYLESHEET("stylesheet"),
    XMLHTTPREQUEST("xmlhttprequest"),
    WEBSOCKET("websocket"),
    WEBRTC("webrtc"),
    PING("ping"),
    OBJECT("object"),
    MEDIA("media"),
    FONT("font"),
    MP4("mp4"),
    OTHER("other"),
    POPUP("popup"),
    ;

    companion object {
        /** `$all` 展开后的集合。 */
        val ALL: Set<ResourceType> = entries.toSet()

        private val byToken: Map<String, ResourceType> =
            entries.associateBy { it.token } + mapOf("xhr" to XMLHTTPREQUEST)

        /** 解析资源类型 token；未知返回 null，由调用方报错到行号。 */
        fun fromToken(token: String): ResourceType? = byToken[token.lowercase()]
    }
}

/**
 * `$domain=` 的作用域：include 为白名单式包含，exclude 为 `~` 前缀的排除。
 * 语义与 uBO 一致：include 非空时请求的文档域名必须命中其一；exclude 命中即整体不适用。
 */
data class DomainScope(
    val include: Set<String>,
    val exclude: Set<String>,
) {
    /** 文档域名是否落在作用域内。documentHost 为 null（如 DNS 层无来源域名）时，仅 include 为空才可通过。 */
    fun allows(documentHost: String?): Boolean {
        val host = documentHost?.lowercase()
        if (host != null && exclude.any { DomainTrie.hostMatchesDomain(host, it) }) return false
        if (include.isEmpty()) return true
        if (host == null) return false
        return include.any { DomainTrie.hostMatchesDomain(host, it) }
    }
}

/** 一条规则的全部修饰符。未出现的修饰符保持默认值。 */
data class RuleModifiers(
    val important: Boolean = false,
    val badFilter: Boolean = false,
    val matchCase: Boolean = false,
    val denyAllow: Set<String> = emptySet(),
    val clientScope: Set<String> = emptySet(),
    val ctagScope: Set<String> = emptySet(),
    val domainScope: DomainScope? = null,
    /** true=仅第三方请求；false=仅第一方请求；null=不限制。 */
    val thirdParty: Boolean? = null,
    val dnsRewrite: String? = null,
    val csp: String? = null,
    val removeParam: String? = null,
    val redirect: String? = null,
    val cookie: String? = null,
    val header: String? = null,
    val method: String? = null,
    val permissions: String? = null,
    /** 空集合表示"适用于全部资源类型"。 */
    val resourceTypes: Set<ResourceType> = emptySet(),
    val elemHide: Boolean = false,
    val genericHide: Boolean = false,
    val specificHide: Boolean = false,
    val genericBlock: Boolean = false,
    val inlineScript: Boolean = false,
    val inlineFont: Boolean = false,
    val empty: Boolean = false,
    /** 原始 token 列表，用于规则体检展示与 round-trip。 */
    val rawTokens: List<String> = emptyList(),
) {
    val isCosmeticControl: Boolean
        get() = elemHide || genericHide || specificHide || genericBlock
}

/**
 * 编译后的匹配模式。
 *
 * 只有两种形态：纯域名后缀匹配（DNS 层可直接命中，走 Trie），
 * 以及编译后的正则。正则统一对"完整 URL（若请求带 URL）否则裸域名"求值，
 * 与 uBO 的匹配模型一致。
 */
sealed interface RulePattern {
    /** `||example.com^` 且不含路径：按域名后缀匹配。 */
    data class DomainOnly(val domain: String) : RulePattern

    /** IPv4 网段（6.4 网络层封禁 / IP-CIDR 分流）。 */
    data class Ipv4Cidr(val cidr: String, val base: Int, val prefix: Int) : RulePattern

    /** 编译后的正则。 */
    data class RegexPattern(val regex: Regex) : RulePattern
}

/** 一条已编译的网络规则。 */
data class CompiledNetworkRule(
    val lineNo: Int,
    val raw: String,
    val sourceId: String,
    val isException: Boolean,
    val pattern: RulePattern,
    /** 用于 Trie 索引的域名提示；null 表示只能线性扫描。 */
    val domainHint: String?,
    val mods: RuleModifiers,
    /** 语义来源类别，供 UI 的来源标签与"规则体检"分类统计。 */
    val kind: RuleKind = RuleKind.FILTER,
) {
    /** 本规则在优先级模型中的层级。 */
    val priority: Priority
        get() = when {
            isException && mods.important -> Priority.IMPORTANT_ALLOW
            !isException && mods.important -> Priority.IMPORTANT_BLOCK
            isException -> Priority.NORMAL_ALLOW
            else -> Priority.NORMAL_BLOCK
        }

    /** 本规则命中后的动作。 */
    val action: Decision
        get() = when {
            isException -> Decision.ALLOW
            mods.dnsRewrite != null -> Decision.REWRITE
            mods.redirect != null -> Decision.REWRITE
            mods.empty -> Decision.REWRITE
            else -> Decision.BLOCK
        }
}

/** 一次匹配请求。url 可为 null（纯 DNS 层查询只有 host）。 */
data class MatchRequest(
    val host: String,
    val url: String? = null,
    val resourceType: ResourceType = ResourceType.OTHER,
    val documentHost: String? = null,
    val client: String? = null,
    val ctag: String? = null,
    val method: String? = null,
    val thirdParty: Boolean? = null,
) {
    companion object {
        /** 由完整 URL 推导出 host 的便捷构造。 */
        fun ofUrl(
            url: String,
            resourceType: ResourceType = ResourceType.OTHER,
            documentHost: String? = null,
        ): MatchRequest = MatchRequest(
            host = extractHost(url),
            url = url,
            resourceType = resourceType,
            documentHost = documentHost,
        )

        /** 从 URL 中提取 host。失败时返回空串，由上层决定是否报错。 */
        fun extractHost(url: String): String {
            val schemeEnd = url.indexOf("://")
            val start = if (schemeEnd >= 0) schemeEnd + 3 else 0
            val rest = url.substring(start)
            val end = rest.indexOfFirst { it == '/' || it == '?' || it == '#' }
            val authority = if (end >= 0) rest.substring(0, end) else rest
            val at = authority.lastIndexOf('@')
            val hostPort = if (at >= 0) authority.substring(at + 1) else authority
            // 处理 IPv6 字面量 [::1]:443
            if (hostPort.startsWith("[")) {
                val close = hostPort.indexOf(']')
                return if (close > 0) hostPort.substring(1, close) else hostPort
            }
            val colon = hostPort.lastIndexOf(':')
            return if (colon > 0) hostPort.substring(0, colon) else hostPort
        }
    }
}

/** 匹配链路中的一条候选记录，供 5.4 的"规则测试"功能展示完整链路。 */
data class ChainEntry(
    val lineNo: Int,
    val raw: String,
    val sourceId: String,
    val priority: Priority,
    val action: Decision,
    val applied: Boolean,
    val skippedReason: String? = null,
)

/** 匹配结果。 */
data class MatchResult(
    val decision: Decision,
    val priority: Priority,
    val matchedRule: CompiledNetworkRule?,
    val chain: List<ChainEntry>,
) {
    val blocked: Boolean get() = decision == Decision.BLOCK
    val allowed: Boolean get() = decision == Decision.ALLOW

    companion object {
        val NONE = MatchResult(Decision.NONE, Priority.NONE, null, emptyList())
    }
}

/** 解析/校验诊断，带行号，供"规则体检"直接呈现。 */
data class RuleDiagnostic(
    val severity: Severity,
    val lineNo: Int,
    val raw: String,
    val message: String,
    val sourceId: String = "",
) {
    enum class Severity { ERROR, WARNING, INFO }
}

/** 单行解析结果。 */
sealed interface ParseOutcome {
    /** 成功解析出一条网络规则。[warnings] 携带"可接受但可疑"的提示（如未识别的修饰符）。 */
    data class Network(
        val rule: CompiledNetworkRule,
        val warnings: List<RuleDiagnostic> = emptyList(),
    ) : ParseOutcome

    /**
     * 一行解析出**多条**网络规则。
     *
     * 存在的唯一理由：hosts 语法 `0.0.0.0 a.test b.test c.test` 一行可含任意多个域名。
     * 旧实现只取第一个域名、其余仅发一条 WARNING —— 规则确实没生效，却只留了条提示，
     * 正是"静默丢弃规则"这一类缺陷（门禁存在的意义就是拦住它）。
     * 单条与多条分成两个类型而不是让 [Network] 携带列表，是为了让"绝大多数行只有一条规则"
     * 这个事实保留在类型里，避免调用方到处写 `rules.first()`。
     */
    data class NetworkMany(
        val rules: List<CompiledNetworkRule>,
        val warnings: List<RuleDiagnostic> = emptyList(),
    ) : ParseOutcome

    /** 成功解析出一条元素隐藏/脚本注入类规则。 */
    data class Cosmetic(val rule: CosmeticRule) : ParseOutcome

    /** 空行、注释、头部声明等无需处理的行。 */
    data class Ignored(val reason: String) : ParseOutcome

    /** 解析失败。 */
    data class Invalid(val diagnostic: RuleDiagnostic) : ParseOutcome
}
