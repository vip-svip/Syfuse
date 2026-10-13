package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.ProxyProtocol

/**
 * Clash / Clash.Meta 配置里的 `proxies:` 段解析。
 *
 * ## 为什么需要它
 *
 * 订阅除了"每行一个分享链接"，还有第二种真实形态：**Clash YAML**。
 * 后者在自建与转发场景里很常见，而 [ShareLink.parseSubscription] 只认 URI 形式 ——
 * 用户粘贴一份 Clash 配置会得到"0 个节点 + N 行错误"，看起来像订阅坏了。
 *
 * ## 为什么不用 YAML 库
 *
 * ① `domain` 的约束是纯 JVM、可离线单测，引入 snakeyaml 这类依赖与
 *    "内核零依赖 / 供应链面最小"的既定方针冲突（见 `src/go.mod` 的同款论证）；
 * ② 这里只需要 `proxies:` 一个段，而 Clash 的 YAML 用法是**受限子集** ——
 *    本类按这个子集精确解析，不为"通用 YAML"付成本；
 * ③ 与 Go 侧共用同一套期望时，行为可控可测比"功能全"更重要。
 *
 * ## 解析范围（刻意只支持这些，其他一律忽略而不是报错）
 *
 * - `proxies:` 顶层键下的列表项（`- name: ...` 起新条目）
 * - `key: value` 标量；单引号 / 双引号 / 无引号三种
 * - 单行 flow 形式：`ws-opts: {path: /ws, headers: {Host: a.com}}`
 * - 嵌套块（缩进更深、无 `-`）→ 折叠成 `父.子` 的点缀键
 * - `#` 注释与空行
 *
 * 不支持（遇到即忽略，不影响其他节点解析）：锚点 / 别名、多行字符串、列表嵌套列表。
 * 这些都是 Clash 配置里几乎不出现的写法。
 */
object ClashProxies {

    /** Clash 的 `type` 取值 → 本项目协议枚举。 */
    fun proxyProtocolOf(type: String): ProxyProtocol? = when (type.lowercase()) {
        "ss" -> ProxyProtocol.SS
        "ssr" -> ProxyProtocol.SSR
        "vmess" -> ProxyProtocol.VMESS
        "vless" -> ProxyProtocol.VLESS
        "trojan" -> ProxyProtocol.TROJAN
        "trojan-go" -> ProxyProtocol.TROJAN_GO
        "hysteria" -> ProxyProtocol.HYSTERIA
        "hysteria2", "hy2" -> ProxyProtocol.HYSTERIA2
        "tuic" -> ProxyProtocol.TUIC
        "snell" -> ProxyProtocol.SNELL
        "http" -> ProxyProtocol.HTTP
        "socks5" -> ProxyProtocol.SOCKS5
        else -> null
    }

    /**
     * 解析 Clash 配置文本，返回 [ShareNode] 列表。
     *
     * **部分成功语义**：无法映射的条目被跳过并计入返回值，不因一条坏项整体失败。
     * 真实 Clash 配置里常混着 `direct` / `reject` 这类**策略组**而不是代理节点 ——
     * 它们是合法配置但不是节点，必须跳过而不是报错。
     *
     * @return 解析出的节点；无 `proxies:` 段或一个都没有时为空列表。
     */
    fun parse(content: String): List<ShareNode> {
        val entries = extractProxiesSection(content)
        val out = mutableListOf<ShareNode>()
        for (e in entries) {
            toShareNode(e)?.let { out += it }
        }
        return out
    }

    /**
     * 取出 `proxies:` 段的所有列表项，每项是一个扁平映射（键已按点缀路径展开）。
     *
     * 只认**顶层** `proxies:`（行首零缩进）。理由：`proxy-groups:` 里可能有
     * 一个名字叫 "proxies" 的组，那显然不是我们想要的段。
     *
     * ## 缩进栈
     *
     * 嵌套块（`ws-opts:` 下再嵌 `headers:`）用**栈**而不是"上一个键"来折叠。
     * 早期实现只记单个 `lastKey`，于是
     * ```
     * ws-opts:
     *   path: /vless
     *   headers:
     *     Host: cdn.example.com
     * ```
     * 会把 Host 折成 `headers.Host` 而不是 `ws-opts.headers.Host` ——
     * 父级在第二层嵌套时丢失。这类缺陷的症状是"能解析、但传输参数没了"，
     * 比直接报错更难发现。现在每帧只存**键片段**，路径由栈内所有片段拼接。
     */
    internal fun extractProxiesSection(content: String): List<Map<String, String>> {
        val lines = content.split('\n')
        val entries = mutableListOf<Map<String, String>>()

        var inSection = false
        var current: MutableMap<String, String>? = null
        // 缩进栈：(缩进列, 键片段)。仅含"值为空的父键"帧。
        val stack = ArrayDeque<Pair<Int, String>>()

        for (raw in lines) {
            // 去掉注释。注意：`#` 只在**行首或空白之后**才算注释；
            // 值内部的 `#`（如 password: ab#cd）必须保留。
            // trimEnd 同时吃掉 CRLF 的 `\r`，因此 Windows 粘贴的配置无需特殊处理。
            val line = stripComment(raw)
            if (line.isBlank()) continue

            val indent = countIndent(line)
            val body = line.trimStart()

            if (!inSection) {
                // 顶层键 `proxies:`（零缩进）。值必须为空否则是 `key: value` 别的键。
                if (indent == 0 && body.startsWith("proxies:")) {
                    val v = body.substringAfter(':', "").trim()
                    if (v.isEmpty()) {
                        inSection = true
                        current = null
                        stack.clear()
                    }
                }
                continue
            }

            // 已进入段内。遇到另一个零缩进顶层键 → 段结束。
            if (indent == 0) {
                inSection = false
                current?.let { entries += it }
                current = null
                stack.clear()
                continue
            }

            if (body.startsWith("- ") || body == "-") {
                // 新条目
                current?.let { entries += it }
                current = mutableMapOf()
                stack.clear()
                val rest = if (body == "-") "" else body.substring(2)
                if (rest.isNotBlank()) {
                    // `- name: x` 或 `- {name: x, ...}`
                    // dash 后的内容等效缩进是 indent + 2（`- ` 占两列）。
                    val keyIndent = indent + 2
                    val wrapped = unwrapFlow(rest)
                    for ((k, v) in wrapped) {
                        // 条目首行不可能有更深缩进的前置帧（栈已清空），路径就是 k 本身。
                        current[k] = v
                        // 压**片段** k（同下方块父键处理的理由）。
                        if (v.isEmpty()) stack.addLast(keyIndent to k)
                    }
                }
                continue
            }

            val target = current
            if (target == null) {
                // 段还没出现 `-` 就有内容 → 异常形态，忽略该行
                continue
            }

            // 弹出所有缩进 **>=** 当前行的帧：那些是兄弟或更深的，与当前键同级。
            while (stack.isNotEmpty() && stack.last().first >= indent) {
                stack.removeLast()
            }

            val wrapped = unwrapFlow(body)
            for ((k, v) in wrapped) {
                val path = if (stack.isEmpty()) k else stack.joinToString(".") { it.second } + "." + k
                target[path] = v
                if (v.isEmpty()) {
                    // 空值 = 块父键，压栈供更深的行拼接。
                    //
                    // ⚠️ 压栈的必须是**键片段** k，不是完整路径 path。
                    // 早先我把 path 压进去，于是 `ws-opts:` 下再嵌 `headers:` 时
                    // 更深的 Host 被拼成 `ws-opts.ws-opts.headers.Host` —— 父级重复了一遍。
                    // 路径由栈内所有片段拼接而成，压完整路径就等于让父亲出现两次。
                    stack.addLast(indent to k)
                }
            }
        }

        current?.let { entries += it }
        return entries
    }

    /**
     * 把一行内容拆成若干 (键, 值)。
     *
     * 处理三种形态：
     *  1. `key: value`        → [(key, value)]
     *  2. `key:`              → [(key, "")]
     *  3. `{k1: v1, k2: v2}`  → [(k1, v1), (k2, v2)]（外层花括号被剥掉）
     *
     * 第 3 种是 `- {name: ..., type: ...}` 整条 flow 条目 —— 少了它，
     * 一整类常见 Clash 导出一个节点都解析不出来（实测：10 条用例全红）。
     */
    private fun unwrapFlow(text: String): List<Pair<String, String>> {
        val t = text.trim()
        if (t.startsWith("{") && t.endsWith("}")) {
            return flattenFlow("", t)
        }
        return flattenKeyValue(t)
    }

    // -----------------------------------------------------------------------
    // 条目 → ShareNode
    // -----------------------------------------------------------------------

    /**
     * 把一个扁平条目映射成 [ShareNode]。
     *
     * 返回 null 表示"这个条目不是可导入的代理节点"（策略组、类型缺失、
     * 协议不认识、缺 server/port 等）。调用方计数但不报错。
     */
    internal fun toShareNode(e: Map<String, String>): ShareNode? {
        val type = e["type"]?.lowercase() ?: return null
        // direct / reject / urltest / fallback / load-balance / select / relay 是策略组，不是节点。
        if (type in KNOWN_GROUP_TYPES) return null
        val protocol = proxyProtocolOf(type) ?: return null

        val server = e["server"].orEmpty().trim()
        if (server.isEmpty()) return null
        val port = e["port"]?.trim()?.toIntOrNull() ?: return null
        if (port <= 0 || port > 65535) return null

        val name = e["name"].orEmpty().trim()
        // Clash 的 udp / skip-cert-verify 等开关
        val insecure = e["skip-cert-verify"].orEmpty().lowercase() in TRUE_SET

        val network = e["network"].orEmpty().trim().ifEmpty { "tcp" }
        // ws-opts / h2-opts / grpc-opts 的取值：既可能是点缀键，也可能是 flow 单行
        val wsPath = pick(e, "ws-opts.path", "ws-opts.headers.Path")
        val wsHost = pick(e, "ws-opts.headers.Host")
        // Clash 的键名是 `grpc-service-name`（注意不是 grpc-name）。
        // 同时认 `grpc-serviceName`：部分导出工具用驼峰，两者都真实存在。
        val grpcSvc = pick(e, "grpc-opts.grpc-service-name", "grpc-opts.grpc-serviceName")
        val path = when {
            wsPath.isNotEmpty() -> wsPath
            grpcSvc.isNotEmpty() -> grpcSvc   // grpc 的 serviceName 复用 path 字段承载
            else -> ""
        }

        // 混淆：Clash 的 ss 用 `plugin:` + `plugin-opts:` 表达
        val plugin = e["plugin"].orEmpty().trim()
        val obfsName = when {
            plugin.isNotEmpty() -> plugin
            e["obfs"]?.isNotEmpty() == true -> e["obfs"]!!
            else -> ""
        }
        val obfsPwd = pick(e, "plugin-opts.obfs-password", "obfs-password")

        return ShareNode(
            name = name,
            protocolRaw = type,
            protocol = protocol,
            server = server,
            port = port,
            password = e["password"].orEmpty(),
            uuid = e["uuid"].orEmpty(),
            network = network,
            tls = e["tls"].orEmpty().lowercase() in TRUE_SET,
            sni = pick(e, "sni", "servername", "peer"),
            host = wsHost.ifEmpty { pick(e, "ws-opts.headers.Host") },
            path = path,
            cipher = e["cipher"].orEmpty(),
            alterId = e["alterId"]?.trim()?.toIntOrNull() ?: 0,
            flow = e["flow"].orEmpty(),
            obfs = obfsName,
            obfsPassword = obfsPwd,
            ssrProtocol = e["protocol"].orEmpty(),
            ssrObfsParam = pick(e, "obfs-param"),
            ssrProtoParam = pick(e, "protocol-param"),
            insecure = insecure,
            // raw 留空：Clash 条目没有"原始 URI"可回溯。
            // 这一条与 ShareNodeMapping 的决策一致 —— raw 会含凭据，不进持久化路径。
            raw = "",
        )
    }

    /** 策略组类型：出现即跳过。 */
    private val KNOWN_GROUP_TYPES = setOf(
        "direct", "reject", "urltest", "fallback", "load-balance", "select", "relay",
    )

    private val TRUE_SET = setOf("true", "yes", "1")

    /** 取第一个非空值。 */
    private fun pick(e: Map<String, String>, vararg keys: String): String =
        keys.firstNotNullOfOrNull { k -> e[k]?.takeIf { it.isNotBlank() } } ?: ""

    // -----------------------------------------------------------------------
    // 词法
    // -----------------------------------------------------------------------

    private fun countIndent(line: String): Int {
        var n = 0
        for (c in line) {
            if (c == ' ') n++ else break
        }
        return n
    }

    /** 去掉行尾注释；引号内的 `#` 不算注释。 */
    internal fun stripComment(line: String): String {
        var inSingle = false
        var inDouble = false
        for (i in line.indices) {
            val c = line[i]
            when (c) {
                '\'' -> if (!inDouble) inSingle = !inSingle
                '"' -> if (!inSingle) inDouble = !inDouble
                '#' -> if (!inSingle && !inDouble) {
                    // `#` 前必须是行首或空白，才算注释（`ab#cd` 是值）
                    if (i == 0 || line[i - 1] == ' ' || line[i - 1] == '\t') {
                        return line.substring(0, i).trimEnd()
                    }
                }
                else -> Unit
            }
        }
        return line.trimEnd()
    }

    /**
     * 解析 `key: value`，flow 映射展开为多条点缀键。
     *
     * 例：`ws-opts: {path: /ws, headers: {Host: a.com}}` 返回
     *   ["ws-opts.path" to "/ws", "ws-opts.headers.Host" to "a.com"]
     *
     * flow 形式在 Clash 配置里**非常常见**（很多工具导出成这种），
     * 不支持它等于大半配置解析不出传输层参数。
     *
     * 空列表表示本行不含可解析的键值分隔，或值为空的块父键（以 `k to ""` 表达）。
     */
    internal fun flattenKeyValue(text: String): List<Pair<String, String>> {
        val idx = indexOfColon(text)
        if (idx < 0) return emptyList()
        val key = text.substring(0, idx).trim().removeSurrounding("\"", "'")
        if (key.isEmpty()) return emptyList()
        val rawValue = text.substring(idx + 1).trim()
        if (rawValue.isEmpty()) return listOf(key to "")

        // 单个键的 flow 映射 → 展开成多条；否则原样返回一个键值对
        if (rawValue.startsWith("{") && rawValue.endsWith("}")) {
            return flattenFlow(key, rawValue)
        }
        return listOf(key to unquote(rawValue))
    }

    /**
     * 展平单行 flow 映射。
     *
     * `ws-opts: {path: /ws, headers: {Host: a.com}}` 传入 key=`ws-opts` 时产出：
     *   ws-opts.path=/ws
     *   ws-opts.headers.Host=a.com
     */
    private fun flattenFlow(prefix: String, flow: String): List<Pair<String, String>> {
        val inner = flow.substring(1, flow.length - 1)
        val out = mutableListOf<Pair<String, String>>()
        // 按顶层逗号切分（括号深度为 0 的逗号）
        for (seg in splitTopLevel(inner, ',')) {
            val s = seg.trim()
            if (s.isEmpty()) continue
            val ci = indexOfColon(s)
            if (ci < 0) continue
            val k = s.substring(0, ci).trim().removeSurrounding("\"", "'")
            val v = s.substring(ci + 1).trim()
            if (k.isEmpty()) continue
            // prefix 为空时（整条 flow 条目）不能拼出 `.key` 这种前导点。
            val full = if (prefix.isEmpty()) k else "$prefix.$k"
            if (v.startsWith("{") && v.endsWith("}")) {
                out += flattenFlow(full, v)
            } else {
                out += full to unquote(v)
            }
        }
        return out
    }

    /** 找键值分隔冒号：跳过引号内的，并要求后跟空白或行尾。 */
    private fun indexOfColon(text: String): Int {
        var inSingle = false
        var inDouble = false
        for (i in text.indices) {
            val c = text[i]
            when (c) {
                '\'' -> if (!inDouble) inSingle = !inSingle
                '"' -> if (!inSingle) inDouble = !inDouble
                ':' -> if (!inSingle && !inDouble) {
                    if (i == text.length - 1 || text[i + 1] == ' ' || text[i + 1] == '\t') return i
                }
            }
        }
        return -1
    }

    /** 按顶层分隔符切分（忽略引号与花括号内的）。 */
    private fun splitTopLevel(text: String, sep: Char): List<String> {
        val out = mutableListOf<String>()
        var depth = 0
        var inSingle = false
        var inDouble = false
        val sb = StringBuilder()
        for (c in text) {
            when (c) {
                '\'' -> if (!inDouble) inSingle = !inSingle
                '"' -> if (!inSingle) inDouble = !inDouble
                '{', '[' -> if (!inSingle && !inDouble) depth++
                '}', ']' -> if (!inSingle && !inDouble) depth--
            }
            if (c == sep && depth == 0 && !inSingle && !inDouble) {
                out += sb.toString()
                sb.clear()
            } else {
                sb.append(c)
            }
        }
        if (sb.isNotEmpty()) out += sb.toString()
        return out
    }

    /** 去引号；单引号按 YAML 语义不转义，双引号解 `\"` 与 `\\`。 */
    internal fun unquote(s: String): String {
        val t = s.trim()
        if (t.length >= 2 && t.startsWith("\"") && t.endsWith("\"")) {
            val body = t.substring(1, t.length - 1)
            return body.replace("\\\"", "\"").replace("\\\\", "\\")
        }
        if (t.length >= 2 && t.startsWith("'") && t.endsWith("'")) {
            // YAML 单引号里 '' 表示一个字面单引号
            return t.substring(1, t.length - 1).replace("''", "'")
        }
        return t
    }
}
