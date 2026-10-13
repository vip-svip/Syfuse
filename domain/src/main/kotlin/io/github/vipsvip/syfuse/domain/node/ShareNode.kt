package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.ProxyProtocol

/**
 * 解析出的节点。
 *
 * 字段与 Go 侧 `Node`（`src/syfuse/node.go`）一一对应，包括那些"协议特有"的可空字段。
 * 之所以不直接复用 `domain.model.ProxyNode`：那个模型有 `id` / `sortIndex` / 加密凭据引用
 * 等**持久化**概念，而本类型只承载"从一条链接解析出来的事实"，两者职责不同。
 * 保存到库时由上层做一次映射（`ShareNode.toProxyNode()`）。
 */
data class ShareNode(
    val name: String = "",
    /** 协议原文（`ss` / `ssr` / `vmess` / `vless` / `trojan` / `hysteria2` / `http` / `socks5`）。 */
    val protocolRaw: String = "",
    val protocol: ProxyProtocol? = null,
    val server: String = "",
    val port: Int = 0,

    // 凭据
    val password: String = "",
    val uuid: String = "",

    // 传输层
    val network: String = "",
    val tls: Boolean = false,
    val sni: String = "",
    val host: String = "",
    val path: String = "",

    // 协议特有
    val cipher: String = "",
    val alterId: Int = 0,
    val flow: String = "",
    val obfs: String = "",

    // 传输层细项
    val obfsPassword: String = "",
    val insecure: Boolean = false,

    // ssr 专有：protocol 与 obfs 是两个独立维度，且各带参数
    val ssrProtocol: String = "",
    val ssrObfsParam: String = "",
    val ssrProtoParam: String = "",

    /** 原始 URI（**含凭据**）。解析出错时用来指出具体哪一行有问题。 */
    val raw: String = "",
) {

    /**
     * 用于去重的稳定标识。
     *
     * 不含 [name] —— 同一个节点在不同订阅里名字可能不同，那不该算两个节点。
     * 含凭据 —— 同服务器同端口但密码不同是两个不同节点。
     *
     * 分隔符与拼接顺序与 Go 侧 `Node.Key()` **逐字一致**：
     * `protocol|server|port|secret|network|path|obfs|ssrProtocol`，全部小写化。
     */
    fun key(): String {
        val secret = if (uuid.isEmpty()) password else uuid
        return listOf(
            protocolRaw.lowercase(),
            server.lowercase(),
            port.toString(),
            secret,
            network.lowercase(),
            path,
            obfs.lowercase(),
            ssrProtocol.lowercase(),
        ).joinToString("|")
    }

    /**
     * 语义相等（round-trip 的判据）。
     *
     * 与 [equals] 的区别：**不比较** [raw] 与 [name]。
     *  - `raw` 是解析输入的回声，序列化后再解析必然不同（序列化会规范化写法）；
     *  - `name` 在 legacy `ss` 格式里可能只存在于 fragment，序列化后落到别的字段位置。
     *
     * 这两个字段都不影响"这个节点连到哪、用什么凭据"—— 它们不影响功能，
     * 因此不该让 round-trip 断言因为它们而失败。
     */
    fun equalsSemantically(other: ShareNode): Boolean =
        protocolRaw == other.protocolRaw &&
            server == other.server &&
            port == other.port &&
            password == other.password &&
            uuid == other.uuid &&
            cipher == other.cipher &&
            alterId == other.alterId &&
            network == other.network &&
            tls == other.tls &&
            sni == other.sni &&
            host == other.host &&
            path == other.path &&
            flow == other.flow &&
            obfs == other.obfs &&
            obfsPassword == other.obfsPassword &&
            insecure == other.insecure &&
            ssrProtocol == other.ssrProtocol &&
            ssrObfsParam == other.ssrObfsParam &&
            ssrProtoParam == other.ssrProtoParam

    /** 逐字段差异，供断言失败时给出可读信息（比一句 `false` 有用得多）。 */
    fun diffFrom(other: ShareNode): List<String> {
        val out = mutableListOf<String>()
        fun cmp(field: String, a: Any?, b: Any?) {
            if (a != b) out += "$field: \"$a\" != \"$b\""
        }
        cmp("protocolRaw", protocolRaw, other.protocolRaw)
        cmp("server", server, other.server)
        cmp("port", port, other.port)
        cmp("password", password, other.password)
        cmp("uuid", uuid, other.uuid)
        cmp("cipher", cipher, other.cipher)
        cmp("alterId", alterId, other.alterId)
        cmp("network", network, other.network)
        cmp("tls", tls, other.tls)
        cmp("sni", sni, other.sni)
        cmp("host", host, other.host)
        cmp("path", path, other.path)
        cmp("flow", flow, other.flow)
        cmp("obfs", obfs, other.obfs)
        cmp("obfsPassword", obfsPassword, other.obfsPassword)
        cmp("insecure", insecure, other.insecure)
        cmp("ssrProtocol", ssrProtocol, other.ssrProtocol)
        cmp("ssrObfsParam", ssrObfsParam, other.ssrObfsParam)
        cmp("ssrProtoParam", ssrProtoParam, other.ssrProtoParam)
        return out
    }

    /**
     * 校验。与 Go 侧 `Node.validate()` 的规则逐条一致。
     *
     * @throws ShareLinkException 校验不过时抛出。
     */
    fun validate() {
        if (server.trim().isEmpty()) throw ShareLinkException("服务器地址为空")
        if (port <= 0 || port > 65535) throw ShareLinkException("端口越界: $port")
        when (protocolRaw) {
            "vmess", "vless" ->
                if (uuid.isEmpty()) throw ShareLinkException("$protocolRaw: UUID 为空")
            "ss", "ssr", "trojan", "hysteria2" ->
                if (password.isEmpty()) throw ShareLinkException("$protocolRaw: 密码为空")
        }
    }

    /** 去掉凭据投影，供 UI 展示（不泄漏口令 / UUID）。 */
    fun redacted(): ShareNode = copy(password = "", uuid = "", raw = "")

    /** UI 展示用标签，例如 `vless · hk1.example.invalid:443`。 */
    val displayLabel: String get() = "$protocolRaw · $server:$port"
}

/**
 * 极简 URI 解析：只取 scheme 之后的 `userinfo@host:port`、查询串与 fragment。
 *
 * 为什么不直接用 `java.net.URI`：它对"scheme 里含 `+` / `.`"、
 * "host 含下划线"、"fragment 需要 percent-decode"等情形的处理与 Go 的 `url.Parse`
 * 有细微差别，而那些差别正好会让两侧解析结果不一致。这里显式实现，行为可控可测。
 */
internal data class SimpleUri(
    val username: String,
    val password: String,
    val hostPort: String,
    val query: Map<String, String>,
    val fragment: String,
) {
    companion object {
        fun parse(uri: String): SimpleUri {
            val schemeEnd = uri.indexOf("://")
            if (schemeEnd <= 0) throw ShareLinkException("缺少 scheme")
            var rest = uri.substring(schemeEnd + 3)

            // fragment
            var fragment = ""
            val hash = rest.indexOf('#')
            if (hash >= 0) {
                fragment = rest.substring(hash + 1)
                rest = rest.substring(0, hash)
                fragment = runCatching {
                    java.net.URLDecoder.decode(fragment, java.nio.charset.StandardCharsets.UTF_8)
                }.getOrNull() ?: fragment
                fragment = fragment.trim()
            }

            // query
            var query: Map<String, String> = emptyMap()
            val qm = rest.indexOf('?')
            if (qm >= 0) {
                query = ShareLink.parseQuery(rest.substring(qm + 1))
                rest = rest.substring(0, qm)
            }

            // userinfo
            var username = ""
            var password = ""
            val at = rest.lastIndexOf('@')
            if (at >= 0) {
                val userinfo = rest.substring(0, at)
                rest = rest.substring(at + 1)
                val colon = userinfo.indexOf(':')
                if (colon >= 0) {
                    username = decodeSafe(userinfo.substring(0, colon))
                    password = decodeSafe(userinfo.substring(colon + 1))
                } else {
                    username = decodeSafe(userinfo)
                }
            }

            return SimpleUri(username, password, rest, query, fragment)
        }

        private fun decodeSafe(s: String): String =
            runCatching {
                java.net.URLDecoder.decode(s, java.nio.charset.StandardCharsets.UTF_8)
            }.getOrDefault(s)
    }
}
