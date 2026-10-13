package io.github.vipsvip.syfuse.domain.node

/**
 * 极简 JSON 读写：只覆盖 vmess 链接体（`base64(JSON)`）所需的标量与字符串取值。
 *
 * ## 为什么不用 kotlinx.serialization
 *
 * vmess 链接体是**外部输入**，字段类型不稳定：`port` / `aid` 在不同客户端里
 * 有时是数字、有时是字符串（`"443"` 与 `443` 都真实存在），甚至可能缺失。
 * 用 `@Serializable data class` 声明 `port: Int` 会在遇到字符串时直接抛解析异常 ——
 * 而这正是 Go 侧 `toInt(any)` 刻意兼容的情形。两侧行为必须一致，
 * 所以这里做成"宽松取值"：拿不到就返回 null，由调用方决定是报错还是取默认值。
 *
 * ## 为什么手写而不是引依赖
 *
 * 本工程对 `domain` 的约束是**纯 JVM、可离线单测**。手写一个只做取值与转义的
 * ~100 行解析器，比引入一个完整 JSON 库更容易论证"行为与 Go 侧一致"。
 */
internal object MiniJson {

    /** 解析为扁平对象：值只保留字符串形态（数字原样转字符串）。 */
    fun parseObject(text: String): JsonObject {
        val p = Parser(text)
        p.skipWs()
        if (p.eof() || p.peek() != '{') throw ShareLinkException("JSON 顶层应为对象")
        val map = p.parseObjectBody()
        p.skipWs()
        return JsonObject(map)
    }

    // -----------------------------------------------------------------------
    // 取值
    // -----------------------------------------------------------------------

    class JsonObject(private val map: Map<String, String?>) {
        /** 取字符串；null / 非字符串一律返回空串（与 Go 的空值语义一致）。 */
        fun string(key: String): String = map[key].orEmpty()

        /** 是否含该键（区分"缺失"与"空串"，少数场景需要）。 */
        fun has(key: String): Boolean = map.containsKey(key)

        /**
         * 取整数，兼容字符串形态。
         *
         * 与 Go 侧 `toInt(any)` 的四种分支（float64 / int / string / json.Number）
         * 语义对齐：数字直接转，数字字符串解析，其余返回 null。
         */
        fun int(key: String): Int? {
            val v = map[key] ?: return null
            val t = v.trim()
            if (t.isEmpty()) return null
            t.toIntOrNull()?.let { return it }
            // 形如 "443.0" / 443.7 -> 截断取整（Go 的 int(float64) 也是截断）
            t.toDoubleOrNull()?.let { return it.toInt() }
            return null
        }
    }

    // -----------------------------------------------------------------------
    // 写
    // -----------------------------------------------------------------------

    fun str(s: String): String = "\"" + escape(s) + "\""

    fun num(n: Int): String = n.toString()

    /** 按给定顺序拼对象字面量（顺序固定 → 序列化结果确定 → 测试可复现）。 */
    fun buildObject(vararg pairs: Pair<String, String>): String =
        pairs.joinToString(",", "{", "}") { (k, v) -> str(k) + ":" + v }

    private fun escape(s: String): String {
        val sb = StringBuilder(s.length + 8)
        for (ch in s) {
            when (ch) {
                '"' -> sb.append("\\\"")
                '\\' -> sb.append("\\\\")
                '\n' -> sb.append("\\n")
                '\r' -> sb.append("\\r")
                '\t' -> sb.append("\\t")
                '\b' -> sb.append("\\b")
                '\u000C' -> sb.append("\\f")
                else -> if (ch < ' ') {
                    sb.append("\\u").append(String.format("%04x", ch.code))
                } else {
                    sb.append(ch)
                }
            }
        }
        return sb.toString()
    }

    // -----------------------------------------------------------------------
    // 解析器
    // -----------------------------------------------------------------------

    private class Parser(private val s: String) {
        private var i = 0
        fun eof(): Boolean = i >= s.length
        fun peek(): Char = s[i]
        fun skipWs() {
            while (i < s.length && s[i].isWhitespace()) i++
        }

        fun parseObjectBody(): Map<String, String?> {
            val out = mutableMapOf<String, String?>()
            require(peek() == '{')
            i++ // consume '{'
            skipWs()
            if (!eof() && peek() == '}') {
                i++
                return out
            }
            while (true) {
                skipWs()
                val key = parseString()
                skipWs()
                if (eof() || s[i] != ':') throw ShareLinkException("JSON 对象缺少 ':'")
                i++
                skipWs()
                out[key] = parseValue()
                skipWs()
                if (eof()) throw ShareLinkException("JSON 对象未闭合")
                when (s[i]) {
                    ',' -> {
                        i++
                    }
                    '}' -> {
                        i++
                        return out
                    }
                    else -> throw ShareLinkException("JSON 对象出现意外字符: '${s[i]}'")
                }
            }
        }

        /** 值统一转成字符串；对象/数组返回 null（vmess 用不到嵌套）。 */
        private fun parseValue(): String? {
            skipWs()
            if (eof()) throw ShareLinkException("JSON 值缺失")
            return when (val c = s[i]) {
                '"' -> parseString()
                '{' -> {
                    skipComposite('{', '}')
                    null
                }
                '[' -> {
                    skipComposite('[', ']')
                    null
                }
                't' -> {
                    expect("true"); "true"
                }
                'f' -> {
                    expect("false"); "false"
                }
                'n' -> {
                    expect("null"); null
                }
                else -> {
                    if (c == '-' || c.isDigit()) parseNumber() else
                        throw ShareLinkException("JSON 值出现意外字符: '$c'")
                }
            }
        }

        private fun skipComposite(open: Char, close: Char) {
            var depth = 0
            var inStr = false
            while (i < s.length) {
                val c = s[i]
                when {
                    inStr -> {
                        if (c == '\\') i++ else if (c == '"') inStr = false
                    }
                    c == '"' -> inStr = true
                    c == open -> depth++
                    c == close -> {
                        depth--
                        if (depth == 0) {
                            i++
                            return
                        }
                    }
                }
                i++
            }
            throw ShareLinkException("JSON 嵌套结构未闭合")
        }

        private fun expect(word: String) {
            if (!s.startsWith(word, i)) throw ShareLinkException("JSON 字面量非法")
            i += word.length
        }

        private fun parseNumber(): String {
            val start = i
            if (!eof() && s[i] == '-') i++
            while (i < s.length && (s[i].isDigit() || s[i] == '.' || s[i] == 'e' || s[i] == 'E' ||
                    s[i] == '+' || s[i] == '-')
            ) {
                i++
            }
            if (i == start) throw ShareLinkException("JSON 数字非法")
            return s.substring(start, i)
        }

        private fun parseString(): String {
            if (eof() || s[i] != '"') throw ShareLinkException("JSON 字符串应以 '\"' 开始")
            i++
            val sb = StringBuilder()
            while (i < s.length) {
                val c = s[i]
                when {
                    c == '"' -> {
                        i++
                        return sb.toString()
                    }
                    c == '\\' -> {
                        i++
                        if (eof()) throw ShareLinkException("JSON 转义未结束")
                        when (val e = s[i]) {
                            '"' -> sb.append('"')
                            '\\' -> sb.append('\\')
                            '/' -> sb.append('/')
                            'b' -> sb.append('\b')
                            'f' -> sb.append('\u000C')
                            'n' -> sb.append('\n')
                            'r' -> sb.append('\r')
                            't' -> sb.append('\t')
                            'u' -> {
                                if (i + 4 >= s.length) throw ShareLinkException("JSON \\u 转义不完整")
                                val hex = s.substring(i + 1, i + 5)
                                val code = hex.toIntOrNull(16)
                                    ?: throw ShareLinkException("JSON \\u 转义非十六进制: $hex")
                                sb.append(code.toChar())
                                i += 4
                            }
                            else -> throw ShareLinkException("JSON 未知转义: \\$e")
                        }
                        i++
                    }
                    else -> {
                        sb.append(c)
                        i++
                    }
                }
            }
            throw ShareLinkException("JSON 字符串未闭合")
        }
    }
}
