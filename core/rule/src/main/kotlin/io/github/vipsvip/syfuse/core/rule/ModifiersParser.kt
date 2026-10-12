package io.github.vipsvip.syfuse.core.rule

/**
 * 修饰符解析。
 *
 * 覆盖 6.1 列出的全部修饰符与资源类型。无法识别的 token 不会被静默丢弃，
 * 而是回传给调用方产出 `WARNING` 级诊断——"规则体检"需要能指出拼错的修饰符，
 * 否则用户会以为规则生效了。
 */
internal object ModifiersParser {

    /** 解析结果：修饰符 + 未识别 token（供诊断）。 */
    data class Result(val mods: RuleModifiers, val unknown: List<String>)

    fun parse(tokens: List<String>): Result {
        var important = false
        var badFilter = false
        var matchCase = false
        var denyAllow = emptySet<String>()
        var clientScope = emptySet<String>()
        var ctagScope = emptySet<String>()
        var domainScope: DomainScope? = null
        var thirdParty: Boolean? = null
        var dnsRewrite: String? = null
        var csp: String? = null
        var removeParam: String? = null
        var redirect: String? = null
        var cookie: String? = null
        var header: String? = null
        var method: String? = null
        var permissions: String? = null
        var types = emptySet<ResourceType>()
        var elemHide = false
        var genericHide = false
        var specificHide = false
        var genericBlock = false
        var inlineScript = false
        var inlineFont = false
        var empty = false
        val unknown = ArrayList<String>()

        for (token in tokens) {
            val eq = token.indexOf('=')
            val name = (if (eq >= 0) token.substring(0, eq) else token).lowercase()
            val value = if (eq >= 0) token.substring(eq + 1) else null

            when (name) {
                "important" -> important = true
                "badfilter" -> badFilter = true
                "match-case" -> matchCase = true
                "denyallow" -> denyAllow = splitList(value)
                "client" -> clientScope = splitList(value)
                "ctag" -> ctagScope = splitList(value)
                "domain" -> domainScope = parseDomainScope(value)
                "third-party", "3p" -> thirdParty = true
                "first-party", "1p" -> thirdParty = false
                "dnsrewrite" -> dnsRewrite = value
                "csp" -> csp = value
                "removeparam" -> removeParam = value
                "redirect" -> redirect = value
                "cookie" -> cookie = value
                "header" -> header = value
                "method" -> method = value
                "permissions" -> permissions = value
                "elemhide" -> elemHide = true
                "generichide" -> genericHide = true
                "specifichide" -> specificHide = true
                "genericblock" -> genericBlock = true
                "inline-script" -> inlineScript = true
                "inline-font" -> inlineFont = true
                "empty" -> empty = true
                "all" -> types = ResourceType.ALL
                else -> {
                    val rt = ResourceType.fromToken(name)
                    if (rt != null && value == null) {
                        types = types + rt
                    } else {
                        unknown.add(token)
                    }
                }
            }
        }

        return Result(
            mods = RuleModifiers(
                important = important,
                badFilter = badFilter,
                matchCase = matchCase,
                denyAllow = denyAllow,
                clientScope = clientScope,
                ctagScope = ctagScope,
                domainScope = domainScope,
                thirdParty = thirdParty,
                dnsRewrite = dnsRewrite,
                csp = csp,
                removeParam = removeParam,
                redirect = redirect,
                cookie = cookie,
                header = header,
                method = method,
                permissions = permissions,
                resourceTypes = types,
                elemHide = elemHide,
                genericHide = genericHide,
                specificHide = specificHide,
                genericBlock = genericBlock,
                inlineScript = inlineScript,
                inlineFont = inlineFont,
                empty = empty,
                rawTokens = tokens,
            ),
            unknown = unknown,
        )
    }

    /** `$domain=` 的 `~` 前缀表示排除，其余为包含。uBO 与 AdGuard 均用 `|` 分隔，这里同时接受 `,`。 */
    private fun parseDomainScope(value: String?): DomainScope? {
        if (value.isNullOrBlank()) return null
        val include = LinkedHashSet<String>()
        val exclude = LinkedHashSet<String>()
        for (part in splitList(value)) {
            if (part.startsWith("~")) {
                val d = part.substring(1)
                if (d.isNotEmpty()) exclude.add(d)
            } else {
                include.add(part)
            }
        }
        return DomainScope(include = include, exclude = exclude)
    }

    private fun splitList(value: String?): Set<String> {
        if (value.isNullOrBlank()) return emptySet()
        return value.split('|', ',')
            .map { it.trim().lowercase() }
            .filter { it.isNotEmpty() }
            .toCollection(LinkedHashSet())
    }

    /** 资源类型 token 集合，供诊断提示可选项。 */
    val resourceTypeTokens: List<String> = ResourceType.entries.map { it.token }
}
