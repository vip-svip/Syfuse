package io.github.vipsvip.syfuse.core.rule

/**
 * 规则解析器：把纯文本规则清单解析为可编译的网络规则与元素类规则。
 *
 * 设计取舍：解析与匹配分离。解析阶段产出带行号的诊断（供"规则体检"直接展示），
 * 匹配阶段只面对已编译结构。这样解析错误永远不会变成运行期静默失效。
 */
object RuleParser {

    /** 一次解析的汇总结果。 */
    data class Report(
        val networkRules: List<CompiledNetworkRule>,
        val cosmeticRules: List<CosmeticRule>,
        val diagnostics: List<RuleDiagnostic>,
        val totalLines: Int,
        val ignoredLines: Int,
    ) {
        val errors: List<RuleDiagnostic>
            get() = diagnostics.filter { it.severity == RuleDiagnostic.Severity.ERROR }

        val warnings: List<RuleDiagnostic>
            get() = diagnostics.filter { it.severity == RuleDiagnostic.Severity.WARNING }
    }

    private val DNSMASQ = Regex("^(address|local)=/([^/]+)/?(.*)$", RegexOption.IGNORE_CASE)
    private val HOSTS_LINE = Regex("^(\\d{1,3}(?:\\.\\d{1,3}){3})\\s+(.+)$")
    private val BLOCK_IPS = setOf("0.0.0.0", "127.0.0.1", "::", "::1")

    /** 解析整份规则文本。 */
    fun parse(
        text: String,
        sourceId: String = "",
        defaultKind: RuleKind = RuleKind.FILTER,
    ): Report {
        val network = ArrayList<CompiledNetworkRule>()
        val cosmetic = ArrayList<CosmeticRule>()
        val diags = ArrayList<RuleDiagnostic>()
        var ignored = 0
        var lineNo = 0
        for (rawLine in text.lineSequence()) {
            lineNo++
            when (val outcome = parseLine(rawLine, lineNo, sourceId, defaultKind)) {
                is ParseOutcome.Network -> {
                    network.add(outcome.rule)
                    diags.addAll(outcome.warnings)
                }
                is ParseOutcome.Cosmetic -> cosmetic.add(outcome.rule)
                is ParseOutcome.Ignored -> ignored++
                is ParseOutcome.Invalid -> diags.add(outcome.diagnostic)
            }
        }
        return Report(network, cosmetic, diags, lineNo, ignored)
    }

    /** 解析单行。 */
    fun parseLine(
        rawLine: String,
        lineNo: Int,
        sourceId: String = "",
        defaultKind: RuleKind = RuleKind.FILTER,
    ): ParseOutcome {
        val line = rawLine.trim()

        if (line.isEmpty()) return ParseOutcome.Ignored("空行")
        if (line.startsWith("[") && line.endsWith("]")) return ParseOutcome.Ignored("列表头声明")
        if (line.startsWith("!") || line.startsWith(";")) return ParseOutcome.Ignored("注释")

        // 以 # 开头：既可能是全局元素规则（##.ad），也可能是 # 注释，用能否解析出标记来区分
        if (line.startsWith("#")) {
            val cosmetic = CosmeticRule.parse(line, lineNo, sourceId)
            return if (cosmetic != null) ParseOutcome.Cosmetic(cosmetic) else ParseOutcome.Ignored("注释")
        }

        // hosts / dnsmasq 专有格式
        parseHostsOrDnsmasq(line, lineNo, sourceId)?.let { return it }

        // 元素类规则：必须在 $ 修饰符切分之前，因为 #$# 自身含 $
        CosmeticRule.parse(line, lineNo, sourceId)?.let { return ParseOutcome.Cosmetic(it) }

        return parseNetwork(line, lineNo, sourceId, defaultKind)
    }

    // ---------------------------------------------------------------- 网络规则

    private fun parseNetwork(
        line: String,
        lineNo: Int,
        sourceId: String,
        defaultKind: RuleKind,
    ): ParseOutcome {
        var text = line
        var isException = false
        if (text.startsWith("@@")) {
            isException = true
            text = text.substring(2)
        }
        if (text.isEmpty()) return invalid(lineNo, line, sourceId, "`@@` 之后没有模式")

        val (patternText, optionTokens) = splitPatternAndModifiers(text)
        if (patternText.isEmpty()) return invalid(lineNo, line, sourceId, "缺少匹配模式")

        val parsedMods = ModifiersParser.parse(optionTokens)
        val warnings = parsedMods.unknown.map {
            RuleDiagnostic(
                RuleDiagnostic.Severity.WARNING,
                lineNo,
                line,
                "未识别的修饰符 `$it`，已忽略。可用资源类型：" +
                    ModifiersParser.resourceTypeTokens.joinToString("/"),
                sourceId,
            )
        }

        val compiled = PatternCompiler.compile(patternText, parsedMods.mods.matchCase)
            ?: return invalid(lineNo, line, sourceId, "无法编译匹配模式 `$patternText`")

        val rule = CompiledNetworkRule(
            lineNo = lineNo,
            raw = line,
            sourceId = sourceId,
            isException = isException,
            pattern = compiled.pattern,
            domainHint = compiled.domainHint,
            mods = parsedMods.mods,
            kind = defaultKind,
        )
        return ParseOutcome.Network(rule, warnings)
    }

    /**
     * 拆分"模式"与"$修饰符"。
     * 正则规则（`/.../`）必须先把闭合斜杠找出来，否则模式里的 `$` 锚点会被误判为修饰符分隔符。
     */
    internal fun splitPatternAndModifiers(text: String): Pair<String, List<String>> {
        if (text.startsWith("/")) {
            for (i in 1 until text.length) {
                if (text[i] == '/' && (i == text.length - 1 || text[i + 1] == '$')) {
                    return text.substring(0, i + 1) to parseOptions(text.substring(i + 1))
                }
            }
        }
        val idx = text.indexOf('$')
        return if (idx < 0) {
            text to emptyList()
        } else {
            text.substring(0, idx) to parseOptions(text.substring(idx))
        }
    }

    private fun parseOptions(rest: String): List<String> {
        if (!rest.startsWith("$")) return emptyList()
        val body = rest.substring(1)
        if (body.isEmpty()) return emptyList()
        return body.split(',').map { it.trim() }.filter { it.isNotEmpty() }
    }

    // ------------------------------------------------------- hosts / dnsmasq

    private fun parseHostsOrDnsmasq(line: String, lineNo: Int, sourceId: String): ParseOutcome? {
        DNSMASQ.matchEntire(line)?.let { m ->
            val directive = m.groupValues[1].lowercase()
            val domain = m.groupValues[2].trim().lowercase()
            val value = m.groupValues[3].trim()
            if (domain.isEmpty()) return invalid(lineNo, line, sourceId, "dnsmasq 规则缺少域名")
            // address=/d/            → 拦截
            // address=/d/1.2.3.4     → 重写到指定地址
            // local=/d/              → 本地应答（不转发上游）
            val mods = when {
                directive == "local" -> RuleModifiers(dnsRewrite = "local", rawTokens = listOf("dnsrewrite=local"))
                value.isEmpty() -> RuleModifiers(rawTokens = emptyList())
                else -> RuleModifiers(dnsRewrite = value, rawTokens = listOf("dnsrewrite=$value"))
            }
            return ParseOutcome.Network(domainRule(domain, line, lineNo, sourceId, RuleKind.HOSTS, mods))
        }

        HOSTS_LINE.matchEntire(line)?.let { m ->
            val ip = m.groupValues[1]
            val hosts = m.groupValues[2].split(Regex("\\s+")).map { it.trim().lowercase() }.filter { it.isNotEmpty() }
            if (hosts.isEmpty()) return invalid(lineNo, line, sourceId, "hosts 行缺少主机名")
            // hosts 一行可挂多个主机名，这里按第一个生成规则，其余追加为同源规则由调用方展开。
            // 为保持"一行一规则"的可追溯性，额外主机名通过 warnings 提示而不是静默丢弃。
            val mods = if (ip in BLOCK_IPS) {
                RuleModifiers()
            } else {
                RuleModifiers(dnsRewrite = ip, rawTokens = listOf("dnsrewrite=$ip"))
            }
            val rule = domainRule(hosts.first(), line, lineNo, sourceId, RuleKind.HOSTS, mods)
            val warnings = if (hosts.size > 1) {
                listOf(
                    RuleDiagnostic(
                        RuleDiagnostic.Severity.WARNING,
                        lineNo,
                        line,
                        "hosts 行含 ${hosts.size} 个主机名，仅 `一条规则对应一个域名` 的模型可表达首个：" +
                            hosts.drop(1).joinToString(", ") + "（已忽略）",
                        sourceId,
                    ),
                )
            } else {
                emptyList()
            }
            return ParseOutcome.Network(rule, warnings)
        }

        return null
    }

    private fun domainRule(
        domain: String,
        raw: String,
        lineNo: Int,
        sourceId: String,
        kind: RuleKind,
        mods: RuleModifiers,
    ): CompiledNetworkRule = CompiledNetworkRule(
        lineNo = lineNo,
        raw = raw,
        sourceId = sourceId,
        isException = false,
        pattern = RulePattern.DomainOnly(domain),
        domainHint = domain,
        mods = mods,
        kind = kind,
    )

    private fun invalid(lineNo: Int, raw: String, sourceId: String, message: String): ParseOutcome.Invalid =
        ParseOutcome.Invalid(
            RuleDiagnostic(RuleDiagnostic.Severity.ERROR, lineNo, raw, message, sourceId),
        )
}
