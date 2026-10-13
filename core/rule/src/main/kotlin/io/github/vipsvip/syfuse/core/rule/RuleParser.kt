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

    // dnsmasq：address=/d/ 拦截；address=/d/ip 重写；local=/d/ 本地应答；
    //          server=/d/ip 指定上游（在过滤语义里与 address=/d/ip 同样表现为"重写"）。
    // 缺 `server` 会让一批真实 dnsmasq 配置被当作"未识别行"静默忽略。
    private val DNSMASQ = Regex("^(address|local|server)=/([^/]+)/?(.*)$", RegexOption.IGNORE_CASE)
    // hosts 行：IPv4 或 IPv6（含 `::` / `::1` / 完整展开形式）作地址。
    // 只写 IPv4 会让 `:: host` 这类行整行落空，而 BLOCK_IPS 里明明登记了 `::` / `::1` ——
    // 集合声称覆盖 IPv6，实际永远走不到，是一处"死代码 + 假覆盖"。
    private val HOSTS_LINE = Regex(
        "^(\\d{1,3}(?:\\.\\d{1,3}){3}|[0-9a-fA-F:]*:[0-9a-fA-F:]+)\\s+(.+)$",
    )
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
        for (rawLine in linesOf(text)) {
            lineNo++
            when (val outcome = parseLine(rawLine, lineNo, sourceId, defaultKind)) {
                is ParseOutcome.Network -> {
                    network.add(outcome.rule)
                    diags.addAll(outcome.warnings)
                }
                is ParseOutcome.NetworkMany -> {
                    network.addAll(outcome.rules)
                    diags.addAll(outcome.warnings)
                }
                is ParseOutcome.Cosmetic -> cosmetic.add(outcome.rule)
                is ParseOutcome.Ignored -> ignored++
                is ParseOutcome.Invalid -> diags.add(outcome.diagnostic)
            }
        }
        return Report(network, cosmetic, diags, lineNo, ignored)
    }

    /**
     * 按 CRLF / LF / CR 切分，并去掉**恰好一个**结尾空段。
     *
     * 不用 `lineSequence()`：它对"以换行结尾"的文本会多产出一个空行
     * （实测 71 行的语料被算成 72 行），使"规则体检"的行数统计比
     * 编辑器与 `wc -l` 多 1。Go 侧 `splitLines` 采用同一约定，两侧必须一致。
     *
     * 只去掉**一个**而不是全部：`a\n\n\n` 按 wc -l 是 3 行（`a` 加两个空行），
     * 全部去掉会算成 1 行。
     */
    internal fun linesOf(text: String): List<String> {
        val out = ArrayList<String>()
        var start = 0
        var i = 0
        while (i < text.length) {
            val c = text[i]
            if (c == '\r' || c == '\n') {
                out.add(text.substring(start, i))
                if (c == '\r' && i + 1 < text.length && text[i + 1] == '\n') i++
                i++
                start = i
            } else {
                i++
            }
        }
        out.add(text.substring(start))
        if (out.isNotEmpty() && out.last().isEmpty()) out.removeAt(out.size - 1)
        return out
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

        // 以 # 开头：既可能是全局元素规则（##.ad），也可能是 # 注释。
        // 判据是"标记必须在行首" —— 只靠"能否解析出标记"会把
        // `# 说明（与 ## 元素规则区分）` 这种注释误判成元素规则，
        // 因为注释正文里出现的 `##` 会被当成标记。
        if (line.startsWith("#")) {
            if (CosmeticRule.startsWithMarker(line)) {
                CosmeticRule.parse(line, lineNo, sourceId)?.let { return ParseOutcome.Cosmetic(it) }
            }
            return ParseOutcome.Ignored("注释")
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
            // hosts 一行可挂多个主机名。**每个主机名都必须产出规则**：
            // 只取第一个 + 打个 WARNING 看着"有提示"，实际效果是把规则静默丢掉了 ——
            // 用户以为整行生效，实际只有第一个域名被拦。这正是门禁要防的
            // "静默丢规则"，所以这里逐域名展开，不再降级为警告。
            val mods = if (ip in BLOCK_IPS) {
                RuleModifiers()
            } else {
                RuleModifiers(dnsRewrite = ip, rawTokens = listOf("dnsrewrite=$ip"))
            }
            val rules = hosts.map { h ->
                domainRule(h, line, lineNo, sourceId, RuleKind.HOSTS, mods)
            }
            return ParseOutcome.NetworkMany(rules)
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
