package io.github.vipsvip.syfuse.core.rule

/**
 * 元素隐藏 / 样式注入 / 脚本注入类规则（6.1 的"元素与修饰规则"）。
 *
 * 应用位置说明（诚实登记，不假装全都能用）：
 *  - [HIDE] / [UNHIDE] / [CSS_INJECT] / [EXTENDED_CSS] 需要渲染上下文才能生效。
 *    本产品主界面是原生 Compose，不承载网页，因此这类规则**只在两个位置生效**：
 *    ① MITM 解密后的 HTML 响应改写；② 可选的局域网面板（默认关闭）。
 *    它们对 DNS 层与连接层拦截没有作用，引擎不会把它们当作网络拦截规则使用。
 *  - [HTML_FILTER] 作用于 MITM 响应体，属于网络层可实现的过滤，优先级最高。
 *  - [SCRIPTLET] 需要脚本引擎（M6 交付）。
 */
enum class CosmeticType {
    HIDE,
    UNHIDE,
    CSS_INJECT,
    EXTENDED_CSS,
    SCRIPTLET,
    HTML_FILTER,
}

/** 一条元素/脚本类规则。 */
data class CosmeticRule(
    val lineNo: Int,
    val raw: String,
    val sourceId: String,
    val type: CosmeticType,
    /** 作用域名集合；include 为空表示全局规则。 */
    val scope: DomainScope,
    /** 选择器、CSS 片段、scriptlet 调用或 HTML 过滤选择器。 */
    val body: String,
) {
    /** 是否为全局规则（无域名限定）。 */
    val isGlobal: Boolean get() = scope.include.isEmpty()

    /** 该规则是否作用于指定文档域名。 */
    fun appliesTo(documentHost: String?): Boolean = scope.allows(documentHost)

    companion object {
        /** 标记按长度降序排列：必须先匹配 `##^`、`#@#` 等多字符标记，否则会被 `##` 抢先切分。 */
        private val MARKERS: List<Pair<String, CosmeticType>> = listOf(
            "##^" to CosmeticType.HTML_FILTER,
            "#@#" to CosmeticType.UNHIDE,
            "#\$#" to CosmeticType.CSS_INJECT,
            "#?#" to CosmeticType.EXTENDED_CSS,
            "#%#" to CosmeticType.SCRIPTLET,
            "##" to CosmeticType.HIDE,
        )

        /**
         * 行首是否紧跟一个元素类标记。
         *
         * 用途：`#` 开头的行既可能是全局元素规则（`##.ad`），也可能是注释。
         * 判据必须是"标记在行首"，不能是"正文里能找出标记" —— 否则
         * `# 说明（与 ## 元素规则区分）` 这种注释会被误判成元素规则。
         * Go 侧移植时发现此缺陷，已在两侧一并修正。
         */
        fun startsWithMarker(line: String): Boolean = MARKERS.any { line.startsWith(it.first) }

        /**
         * 尝试解析为元素类规则。返回 null 表示本行不是元素类规则。
         * 注意调用顺序：本方法必须在 `$` 修饰符切分**之前**调用，因为 `#$#` 自身含 `$`。
         */
        fun parse(line: String, lineNo: Int, sourceId: String): CosmeticRule? {
            for ((marker, type) in MARKERS) {
                val idx = line.indexOf(marker)
                if (idx < 0) continue
                val domainPart = line.substring(0, idx)
                val body = line.substring(idx + marker.length)
                if (body.isEmpty()) return null
                return CosmeticRule(
                    lineNo = lineNo,
                    raw = line,
                    sourceId = sourceId,
                    type = type,
                    scope = parseScope(domainPart),
                    body = body,
                )
            }
            return null
        }

        /** 逗号分隔，`~` 前缀为排除。空串表示全局。 */
        private fun parseScope(domainPart: String): DomainScope {
            if (domainPart.isBlank()) return DomainScope(emptySet(), emptySet())
            val include = LinkedHashSet<String>()
            val exclude = LinkedHashSet<String>()
            for (part in domainPart.split(',')) {
                val d = part.trim().lowercase()
                if (d.isEmpty()) continue
                if (d.startsWith("~")) {
                    val stripped = d.substring(1)
                    if (stripped.isNotEmpty()) exclude.add(stripped)
                } else {
                    include.add(d)
                }
            }
            return DomainScope(include, exclude)
        }
    }
}
