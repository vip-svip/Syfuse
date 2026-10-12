package io.github.vipsvip.syfuse.core.rule

/**
 * 把 uBO 风格的规则模式编译为 [RulePattern]。
 *
 * 支持的形态（判定顺序即代码顺序，互斥）：
 *  1. `/正则/`              —— 显式正则
 *  2. `||domain^`           —— 域名锚定；不含路径时降为 Trie 可索引的 DomainOnly
 *  3. `http(s)://...` / `|...` —— 协议或起始锚定的 URL 模式
 *  4. `example.com`         —— 合法主机名，按纯域名（hosts 语义）处理
 *  5. 含正则元字符的模式     —— 正则（覆盖规范里 `ads?banner\d+/` 这类写法）
 *  6. 其余                  —— 通配模式（`*` → `.*`，`^` → 分隔符）
 *
 * 通配模式的元字符语义与 uBO 一致：
 *  - `*` 匹配任意长度字符
 *  - `^` 匹配"分隔符"：任何非 `[a-zA-Z0-9_-.%]` 的字符，或字符串结尾
 */
internal object PatternCompiler {
    /** uBO 的分隔符类。 */
    private const val SEPARATOR = "(?:[^a-zA-Z0-9_\\-.%]|$)"

    /** 主机名标签字符。 */
    private const val HOST_CHARS = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_"

    private val IPV4_CIDR = Regex("^(\\d{1,3}(?:\\.\\d{1,3}){3})(?:/(\\d{1,2}))?$")

    data class Compiled(val pattern: RulePattern, val domainHint: String?)

    /** 编译失败返回 null；调用方负责产出带行号的诊断。 */
    fun compile(pattern: String, matchCase: Boolean): Compiled? {
        if (pattern.isEmpty()) return null
        val flags = if (matchCase) emptySet() else setOf(RegexOption.IGNORE_CASE)

        // 0) IPv4 网段：`1.2.3.4` 与 `1.2.3.0/24`。必须在主机名判定之前，
        //    否则点分十进制会被当作普通域名塞进 Trie，丢失网段语义。
        IPV4_CIDR.matchEntire(pattern)?.let { m ->
            val base = Ipv4CidrTrie.parseIpv4(m.groupValues[1]) ?: return null
            val prefix = m.groupValues[2].takeIf { it.isNotEmpty() }?.toIntOrNull() ?: 32
            if (prefix !in 0..32) return null
            return Compiled(RulePattern.Ipv4Cidr(pattern, base, prefix), null)
        }

        // 1) 显式正则
        if (pattern.length > 2 && pattern.startsWith("/") && pattern.endsWith("/")) {
            val body = pattern.substring(1, pattern.length - 1)
            return regex(body, flags)?.let { Compiled(RulePattern.RegexPattern(it), null) }
        }

        // 2) 域名锚定 ||
        if (pattern.startsWith("||")) {
            return compileDomainAnchored(pattern.substring(2), flags)
        }

        // 3) 协议锚定 / 起始锚定
        if (pattern.startsWith("|")) {
            val body = pattern.substring(1)
            val anchorEnd = body.endsWith("|")
            val core = if (anchorEnd) body.dropLast(1) else body
            return regex(wildcardBody(core, anchorStart = true, anchorEnd = anchorEnd), flags)
                ?.let { Compiled(RulePattern.RegexPattern(it), null) }
        }
        if (pattern.startsWith("http://") || pattern.startsWith("https://")) {
            return regex(wildcardBody(pattern, anchorStart = true, anchorEnd = false), flags)
                ?.let { Compiled(RulePattern.RegexPattern(it), null) }
        }

        // 4) 裸主机名 → 纯域名
        if (isValidHostname(pattern)) {
            val d = pattern.lowercase()
            return Compiled(RulePattern.DomainOnly(d), d)
        }

        // 5) 含正则元字符 → 正则
        if (looksLikeRegex(pattern)) {
            return regex(pattern, flags)?.let { Compiled(RulePattern.RegexPattern(it), null) }
        }

        // 6) 通配模式
        return regex(wildcardBody(pattern, anchorStart = false, anchorEnd = false), flags)
            ?.let { Compiled(RulePattern.RegexPattern(it), null) }
    }

    private fun compileDomainAnchored(rest: String, flags: Set<RegexOption>): Compiled? {
        val domain = readDomainPrefix(rest) ?: return null
        val remainder = rest.substring(domain.length)
        val lower = domain.lowercase()

        // 纯域名形态：交给 Trie，DNS 层 O(标签数) 命中
        if (remainder.isEmpty() || remainder == "^") {
            return Compiled(RulePattern.DomainOnly(lower), lower)
        }

        // 带路径/通配：锚定在域名标签边界，避免 example.com.evil.com 被误命中
        val body = buildString {
            append("^([a-z][a-z0-9+.\\-]*:)?//(?:[^/?#@]*@)?(?:[^/?#]*\\.)?")
            append(Regex.escape(lower))
            append(wildcardBody(remainder, anchorStart = false, anchorEnd = false))
        }
        return regex(body, flags)?.let { Compiled(RulePattern.RegexPattern(it), lower) }
    }

    /**
     * 从 `||` 之后读取域名前缀。遇到 `/`、`^`、`*`、`?`、`|`、`$` 停止。
     * 结果末尾的点会被去掉（`||example.*` 这类写法）。
     */
    private fun readDomainPrefix(rest: String): String? {
        var i = 0
        while (i < rest.length && HOST_CHARS.indexOf(rest[i]) >= 0) i++
        val raw = rest.substring(0, i).trimEnd('.')
        if (raw.isEmpty()) return null
        return raw
    }

    /** 生成通配模式的**正则体**（未编译）。 */
    private fun wildcardBody(body: String, anchorStart: Boolean, anchorEnd: Boolean): String {
        val sb = StringBuilder(body.length + 16)
        if (anchorStart) sb.append('^')
        for (c in body) {
            when (c) {
                '*' -> sb.append(".*")
                '^' -> sb.append(SEPARATOR)
                else -> sb.append(Regex.escape(c.toString()))
            }
        }
        if (anchorEnd) sb.append("\\$")
        return sb.toString()
    }

    private fun regex(body: String, flags: Set<RegexOption>): Regex? =
        runCatching { Regex(body, flags) }.getOrNull()

    /**
     * 是否是合法主机名：至少一个点、每段非空、仅含字母数字与 `-`、不以 `-` 开头或结尾。
     * 这是"纯域名清单"与"通配模式"的分界线。
     */
    fun isValidHostname(text: String): Boolean {
        if (text.isEmpty() || text.length > 253) return false
        if (text.contains('*') || text.contains('^')) return false
        val labels = text.split('.')
        if (labels.size < 2) return false
        for (label in labels) {
            if (label.isEmpty() || label.length > 63) return false
            if (label.startsWith('-') || label.endsWith('-')) return false
            if (!label.all { it.isLetterOrDigit() || it == '-' }) return false
        }
        return true
    }

    /** 出现正则专属构造时按正则处理。这是对规范中 `ads?banner\d+/` 这类写法的兼容路径。 */
    private fun looksLikeRegex(text: String): Boolean {
        if (text.contains("\\d") || text.contains("\\w") || text.contains("\\s") || text.contains("\\b")) return true
        if (text.contains('(') || text.contains('[') || text.contains('{')) return true
        // 以 / 结尾且含正则量词：ads?banner\d+/ 这类
        return text.endsWith("/") && (text.contains('?') || text.contains('+') || text.contains('*'))
    }
}
