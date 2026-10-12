package io.github.vipsvip.syfuse.core.rule

/**
 * 过滤规则匹配引擎。
 *
 * 核心语义（6.1）：
 * ```
 * 重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中（默认放行）
 * ```
 * 引擎**不做先命中即返回**的短路。它先收集全部候选规则，再按 [Priority.rank] 取最大者。
 * 原因：短路会破坏 `$important` 与 `@@` 的表达能力——例如一条普通拦截规则排在前面时，
 * 后面的重要放行规则将永远没有机会被考虑。
 *
 * 索引结构：
 *  - 域名类规则 → [DomainTrie]，按标签深度查找，命中成本与规则总数无关；
 *  - 网段类规则 → [Ipv4CidrTrie]，最长前缀匹配；
 *  - 其余（含通配、正则、带路径的域名锚定）→ 线性扫描列表。
 *
 * 复杂度诚实说明：线性扫描列表的规模取决于规则集的写法。纯域名规则集可全部进 Trie，
 * 此时单次匹配为 O(标签数)。含大量正则的规则集则退化为线性，这是 uBO 系引擎的固有特性，
 * 不是本实现的缺陷。第 11 部分的性能预算按"以域名类规则为主"的规则集测量。
 */
class FilterEngine private constructor(
    private val allRules: List<CompiledNetworkRule>,
    private val domainIndex: DomainTrie<CompiledNetworkRule>,
    private val cidrIndex: Ipv4CidrTrie<MutableList<CompiledNetworkRule>>,
    private val linearRules: List<CompiledNetworkRule>,
    /** 编译期诊断：$badfilter 生效记录、规则冲突等。 */
    val diagnostics: List<RuleDiagnostic>,
) {
    val ruleCount: Int get() = allRules.size

    /** 线性扫描列表的规模，用于评估规则集写法对性能的影响。 */
    val linearRuleCount: Int get() = linearRules.size

    /** 索引化规则数（域名 Trie + 网段 Trie）。 */
    val indexedRuleCount: Int get() = allRules.size - linearRules.size

    /**
     * 匹配一次请求。
     *
     * [collectChain] 为 true 时返回完整匹配链路（供 5.4"规则测试"展示）；
     * 热路径应保持 false，避免为每次匹配分配链路对象。
     */
    fun match(request: MatchRequest, collectChain: Boolean = false): MatchResult {
        val candidates = collectCandidates(request)
        var best: CompiledNetworkRule? = null
        var bestRank = -1
        val chain: MutableList<ChainEntry>? = if (collectChain) ArrayList(candidates.size) else null

        for (rule in candidates) {
            val skip = skipReason(rule, request)
            if (skip != null) {
                chain?.add(entry(rule, applied = false, reason = skip))
                continue
            }
            val hit = patternMatches(rule, request)
            chain?.add(entry(rule, applied = hit, reason = if (hit) null else "模式未命中"))
            if (!hit) continue
            val rank = rule.priority.rank
            if (rank > bestRank) {
                bestRank = rank
                best = rule
            }
        }

        return if (best == null) {
            MatchResult(Decision.NONE, Priority.NONE, null, chain ?: emptyList())
        } else {
            MatchResult(best.action, best.priority, best, chain ?: emptyList())
        }
    }

    /**
     * 该文档域名下的元素隐藏类控制位（`$elemhide` / `$generichide` / `$specifichide` / `$genericblock`）。
     * 这些修饰符不参与网络拦截判定，必须单独查询，否则会错误地把放行规则当成拦截规则。
     */
    fun cosmeticControlFor(documentHost: String, request: MatchRequest? = null): CosmeticControl {
        val probe = request ?: MatchRequest(host = documentHost, documentHost = documentHost)
        var result = CosmeticControl()
        for (rule in collectCandidates(probe)) {
            if (!rule.isException || !rule.mods.isCosmeticControl) continue
            if (skipReason(rule, probe) != null) continue
            if (!patternMatches(rule, probe)) continue
            if (rule.mods.elemHide) result = result.copy(disableElemHide = true)
            if (rule.mods.genericHide) result = result.copy(disableGenericHide = true)
            if (rule.mods.specificHide) result = result.copy(disableSpecificHide = true)
            if (rule.mods.genericBlock) result = result.copy(disableGenericBlock = true)
        }
        return result
    }

    // ------------------------------------------------------------------ 内部

    private fun collectCandidates(request: MatchRequest): List<CompiledNetworkRule> {
        val host = request.host
        val indexed = ArrayList<CompiledNetworkRule>(8)
        domainIndex.lookup(host).let { indexed.addAll(it) }
        // 仅当请求主机本身是 IPv4 字面量时才查网段索引，避免把域名解析结果误当 IP
        if (Ipv4CidrTrie.parseIpv4(host) != null) {
            cidrIndex.lookup(host)?.let { indexed.addAll(it) }
        }
        if (linearRules.isEmpty()) return indexed
        val out = ArrayList<CompiledNetworkRule>(indexed.size + linearRules.size)
        out.addAll(indexed)
        out.addAll(linearRules)
        return out
    }

    /** 返回 null 表示该规则适用于本请求；否则返回跳过原因（用于链路展示）。 */
    private fun skipReason(rule: CompiledNetworkRule, req: MatchRequest): String? {
        val mods = rule.mods

        // 元素隐藏控制类修饰符不参与网络拦截
        if (mods.isCosmeticControl) return "元素隐藏控制类修饰符，不参与网络拦截"

        if (mods.resourceTypes.isNotEmpty() && req.resourceType !in mods.resourceTypes) {
            return "资源类型不匹配（要求 ${mods.resourceTypes.joinToString("/") { it.token }}）"
        }
        if (mods.thirdParty != null && req.thirdParty != null && mods.thirdParty != req.thirdParty) {
            return if (mods.thirdParty) "规则仅适用于第三方请求" else "规则仅适用于第一方请求"
        }
        if (mods.clientScope.isNotEmpty() && req.client?.lowercase() !in mods.clientScope) {
            val allowed = mods.clientScope.joinToString("|")
            return "客户端标识不匹配（本机未在 $allowed 中）"
        }
        if (mods.ctagScope.isNotEmpty() && req.ctag?.lowercase() !in mods.ctagScope) {
            val allowed = mods.ctagScope.joinToString("|")
            return "标签组不匹配（本机未在 $allowed 中）"
        }
        if (mods.domainScope != null && !mods.domainScope.allows(req.documentHost)) {
            return "来源域名不在 \$domain 作用域内"
        }
        if (mods.method != null && !mods.method.equals(req.method, ignoreCase = true)) {
            return "请求方法不匹配（要求 ${mods.method}）"
        }
        // $denyallow：规则对列出的域名不生效
        if (mods.denyAllow.isNotEmpty() && mods.denyAllow.any { DomainTrie.hostMatchesDomain(req.host, it) }) {
            return "\$denyallow 将该域名排除在本规则之外"
        }
        return null
    }

    private fun patternMatches(rule: CompiledNetworkRule, req: MatchRequest): Boolean =
        when (val p = rule.pattern) {
            is RulePattern.DomainOnly -> DomainTrie.hostMatchesDomain(req.host, p.domain)
            is RulePattern.Ipv4Cidr -> {
                val addr = Ipv4CidrTrie.parseIpv4(req.host) ?: return false
                val mask = if (p.prefix == 0) 0 else (-1 shl (32 - p.prefix))
                (addr and mask) == (p.base and mask)
            }
            is RulePattern.RegexPattern -> p.regex.containsMatchIn(req.url ?: req.host)
        }

    private fun entry(rule: CompiledNetworkRule, applied: Boolean, reason: String?): ChainEntry =
        ChainEntry(
            lineNo = rule.lineNo,
            raw = rule.raw,
            sourceId = rule.sourceId,
            priority = rule.priority,
            action = rule.action,
            applied = applied,
            skippedReason = reason,
        )

    companion object {
        /**
         * 编译规则集。
         *
         * `$badfilter` 的处理放在编译期而不是匹配期：它改变的是**规则集的构成**，
         * 在匹配期处理会让"被抵消"的规则仍然参与扫描，白白浪费预算。
         */
        fun compile(
            rules: List<CompiledNetworkRule>,
            extraDiagnostics: List<RuleDiagnostic> = emptyList(),
        ): FilterEngine {
            val diagnostics = ArrayList<RuleDiagnostic>(extraDiagnostics)

            // 1) 收集 $badfilter 签名
            val badSignatures = LinkedHashMap<String, CompiledNetworkRule>()
            for (rule in rules) {
                if (rule.mods.badFilter) {
                    badSignatures[signatureOf(rule)] = rule
                }
            }

            // 2) 剔除被抵消的规则（含 $badfilter 规则自身）
            val effective = ArrayList<CompiledNetworkRule>(rules.size)
            var disabled = 0
            for (rule in rules) {
                val sig = signatureOf(rule)
                val killer = badSignatures[sig]
                if (killer != null) {
                    disabled++
                    diagnostics.add(
                        RuleDiagnostic(
                            RuleDiagnostic.Severity.INFO,
                            killer.lineNo,
                            killer.raw,
                            "`\$badfilter` 已关闭第 ${rule.lineNo} 行的同形规则",
                            killer.sourceId,
                        ),
                    )
                    continue
                }
                effective.add(rule)
            }

            // 3) 建索引
            val domainIndex = DomainTrie<CompiledNetworkRule>()
            val cidrIndex = Ipv4CidrTrie<MutableList<CompiledNetworkRule>>()
            // 同一 CIDR 可以挂多条规则，Trie 节点只能存一个值，因此用桶列表承载。
            val cidrBuckets = HashMap<String, MutableList<CompiledNetworkRule>>()
            val linear = ArrayList<CompiledNetworkRule>()
            for (rule in effective) {
                when (val p = rule.pattern) {
                    is RulePattern.DomainOnly -> domainIndex.put(p.domain, rule)
                    is RulePattern.Ipv4Cidr -> {
                        val bucket = cidrBuckets.getOrPut(p.cidr) {
                            ArrayList<CompiledNetworkRule>(1).also { cidrIndex.put(p.cidr, it) }
                        }
                        bucket.add(rule)
                    }
                    is RulePattern.RegexPattern -> {
                        if (rule.domainHint != null) {
                            domainIndex.put(rule.domainHint, rule)
                        } else {
                            linear.add(rule)
                        }
                    }
                }
            }

            diagnostics.add(
                RuleDiagnostic(
                    RuleDiagnostic.Severity.INFO,
                    0,
                    "",
                    "规则编译完成：总计 ${rules.size} 条，$disabled 条被 \$badfilter 关闭，" +
                        "索引化 ${effective.size - linear.size} 条，线性扫描 ${linear.size} 条",
                ),
            )

            return FilterEngine(effective, domainIndex, cidrIndex, linear, diagnostics)
        }

        /**
         * 规则签名：用于 `$badfilter` 的同形判定。
         * 由**结构化字段**重建，而不是从原始文本裁剪，否则正则规则里的 `$` 锚点会导致误判。
         */
        internal fun signatureOf(rule: CompiledNetworkRule): String {
            val patternKey = when (val p = rule.pattern) {
                is RulePattern.DomainOnly -> "D:${p.domain}"
                is RulePattern.Ipv4Cidr -> "C:${p.cidr}"
                is RulePattern.RegexPattern -> "R:${p.regex.pattern}"
            }
            val tokens = rule.mods.rawTokens
                .filterNot { it.equals("badfilter", ignoreCase = true) }
                .sorted()
                .joinToString(",")
            return "${if (rule.isException) "E" else "B"}|$patternKey|$tokens"
        }
    }
}

/** 文档级元素隐藏控制位。 */
data class CosmeticControl(
    val disableElemHide: Boolean = false,
    val disableGenericHide: Boolean = false,
    val disableSpecificHide: Boolean = false,
    val disableGenericBlock: Boolean = false,
)
