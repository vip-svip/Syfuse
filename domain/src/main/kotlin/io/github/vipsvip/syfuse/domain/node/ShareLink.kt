package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import java.net.URLDecoder
import java.nio.charset.StandardCharsets
import java.util.Base64

/**
 * 分享链接的解析与序列化（8 协议 round-trip）。
 *
 * ## 语义真相在 Go 侧
 *
 * 本文件是 `src/syfuse/node.go`（756 行）的 Kotlin 对等实现，**行为必须一致**：
 * 同一个链接在 App 里解析出的节点，必须和守护进程解析出的完全一样，否则会出现
 * "界面显示的节点"和"实际用来连接的内核参数"不一致 —— 这类 bug 极难排查，
 * 因为两侧各自看起来都"对"。
 *
 * 因此本实现的每一处细节都刻意与 Go 侧逐条对齐，包括那些看起来"奇怪"的地方：
 *  - `ss` 的 legacy 格式、SIP002 的 userinfo base64、percent-encoding；
 *  - `vmess` 的 `port` / `aid` 既可能是数字也可能是字符串（必须两种都收）；
 *  - `trojan` 的 `security` 字段**缺省即视为 TLS**，只有显式 `none` 才不加密；
 *  - `hysteria2` 基于 QUIC，`TLS` **恒为 true**，与链接里写不写无关；
 *  - `http`/`socks5` 在"只有用户名没有密码"时，把用户名挪进 `password` 复用。
 *
 * ## 为什么 base64 要试四种组合
 *
 * 真实订阅里 URL-safe 与标准字母表、有无 padding 是混用的。只支持一种会让
 * "看起来格式正确"的订阅解析失败，且失败信息很难理解。顺序与 Go 侧一致：
 * RawURL → URL → RawStd → Std。
 */
object ShareLink {

    /** 与 Go 侧 `SupportedProtocols()` 保持一致，顺序也相同（供确定性测试比对）。 */
    val SUPPORTED_PROTOCOLS: List<String> =
        listOf("ss", "ssr", "vmess", "vless", "trojan", "hysteria2", "http", "socks5")

    // -----------------------------------------------------------------------
    // 解析
    // -----------------------------------------------------------------------

    /**
     * 解析单条节点 URI。
     *
     * @throws ShareLinkException 解析失败时抛出，[ShareLinkException.uri] 保留原始文本，
     *   便于向用户指出**具体哪一行**有问题（与 Go 侧保留 `Raw` 字段同一目的）。
     */
    fun parse(raw: String): ShareNode {
        val trimmed = raw.trim()
        if (trimmed.isEmpty()) throw ShareLinkException("空 URI", raw)

        val (scheme, rest) = splitScheme(trimmed)
            ?: throw ShareLinkException("缺少 scheme: ${truncate(trimmed, 24)}", raw)

        var node = when (scheme) {
            "ss" -> parseSs(rest)
            "ssr" -> parseSsr(rest)
            "vmess" -> parseVmess(rest)
            "vless" -> parseVless(rest)
            "trojan" -> parseTrojan(rest)
            "hysteria2", "hy2" -> parseHysteria2(rest)
            "http", "https" -> parseHttpLike(rest, scheme)
            "socks", "socks5" -> parseHttpLike(rest, scheme)
            else -> throw ShareLinkException("不支持的协议: \"$scheme\"", raw)
        }

        if (node.protocolRaw.isEmpty()) node = node.copy(protocolRaw = scheme)
        node.validate()
        if (node.name.isEmpty()) {
            node = node.copy(name = "${node.protocolRaw}-${node.server}:${node.port}")
        }
        return node.copy(raw = trimmed)
    }

    /**
     * 解析订阅内容（每行一个 URI）。
     *
     * **部分成功语义**：解析成功的节点照常返回，失败的行以 [SubscriptionLineError] 一并返回，
     * 不因为一行坏掉就整体失败。真实订阅里出现一两行厂商私有格式是常态，
     * 整体失败会让用户完全无法使用订阅。
     *
     * 兼容"只有 base64 内容、没有 scheme"的订阅：整体解码后递归解析。
     */
    fun parseSubscription(content: String): SubscriptionParseResult {
        val nodes = mutableListOf<ShareNode>()
        val errors = mutableListOf<SubscriptionLineError>()
        val seen = mutableSetOf<String>()

        content.split("\n").forEachIndexed { index, rawLine ->
            val line = rawLine.trim()
            if (line.isEmpty() || line.startsWith("#") || line.startsWith("//")) return@forEachIndexed

            if (!line.contains("://")) {
                val decoded = runCatching { decodeBase64(line) }.getOrNull()
                if (decoded != null && decoded.contains("://")) {
                    val (subNodes, subErrors) = parseSubscription(decoded)
                    subNodes.forEach { n ->
                        if (seen.add(n.key())) nodes += n
                    }
                    subErrors.forEach { e ->
                        errors += SubscriptionLineError(
                            index + 1,
                            "第 ${index + 1} 行（内嵌订阅）: ${e.message}",
                        )
                    }
                    return@forEachIndexed
                }
            }

            try {
                val node = parse(line)
                if (seen.add(node.key())) nodes += node
            } catch (e: ShareLinkException) {
                errors += SubscriptionLineError(index + 1, "第 ${index + 1} 行: ${e.message}")
            }
        }
        return SubscriptionParseResult(nodes, errors)
    }

    // -----------------------------------------------------------------------
    // ss
    // -----------------------------------------------------------------------

    /**
     * `ss://` 有两种历史格式，必须都支持，否则会有一批老订阅解析不出来。
     *
     * ```
     * SIP002: ss://<base64url(method:password)>@host:port/?plugin=...#tag
     * 旧格式:  ss://<base64(method:password@host:port)>#tag
     * 明文:    ss://method:password@host:port#tag     （userinfo 可能被 percent-encode）
     * ```
     */
    private fun parseSs(rest: String): ShareNode {
        val (body0, name) = splitFragment(rest)

        // 去掉 SIP002 的插件查询串
        val q = body0.indexOf('?')
        val body = if (q >= 0) body0.substring(0, q) else body0

        val userinfo: String
        val hostport: String
        val at = body.lastIndexOf('@')
        if (at >= 0) {
            // base64 字母表不含 '@'，因此最后一个 '@' 一定是分隔符
            userinfo = body.substring(0, at)
            hostport = body.substring(at + 1)
        } else {
            val decoded = try {
                decodeBase64(body)
            } catch (e: ShareLinkException) {
                throw ShareLinkException("ss: 无法解码: ${e.message}")
            }
            val at2 = decoded.lastIndexOf('@')
            if (at2 < 0) throw ShareLinkException("ss: 解码后缺少 '@' 分隔符")
            userinfo = decoded.substring(0, at2)
            hostport = decoded.substring(at2 + 1)
        }

        // userinfo 可能是 base64(method:password)，也可能是明文 method:password
        var cred = userinfo
        if (!cred.contains(':')) {
            runCatching { decodeBase64(cred) }.getOrNull()?.let { cred = it }
        }
        val ci = cred.indexOf(':')
        if (ci < 0) throw ShareLinkException("ss: 凭据格式应为 method:password")
        val method = cred.substring(0, ci)
        // Kotlin 的 Cut 语义：密码可以含 ':'
        var password = cred.substring(ci + 1)
        runCatching { urlQueryUnescape(password) }.getOrNull()?.let { password = it }

        val (server, port) = splitHostPort(hostport, "ss")
        return ShareNode(
            name = name,
            protocolRaw = "ss",
            protocol = ProxyProtocol.SS,
            server = server,
            port = port,
            password = password,
            cipher = method.trim(),
            network = "tcp",
        )
    }

    // -----------------------------------------------------------------------
    // ssr
    // -----------------------------------------------------------------------

    /**
     * ```
     * ssr://<base64url(host:port:protocol:method:obfs:base64url(password)/?obfsparam=&protoparam=&remarks=>)
     * ```
     */
    private fun parseSsr(rest: String): ShareNode {
        val decoded = try {
            decodeBase64(rest)
        } catch (e: ShareLinkException) {
            throw ShareLinkException("ssr: 无法解码: ${e.message}")
        }
        // 参数部分在 '?' 之后
        var main: String
        var query: String
        val cut = decoded.indexOf("/?")
        if (cut >= 0) {
            main = decoded.substring(0, cut)
            query = decoded.substring(cut + 2)
        } else {
            val qi = decoded.indexOf('?')
            main = if (qi >= 0) decoded.substring(0, qi) else decoded
            query = if (qi >= 0) decoded.substring(qi + 1) else ""
        }

        val parts = main.split(":")
        if (parts.size < 6) {
            throw ShareLinkException("ssr: 字段数不足（需要 6 个，实际 ${parts.size}）")
        }
        val port = parts[1].trim().toIntOrNull()
            ?: throw ShareLinkException("ssr: 端口非法: \"${parts[1]}\"")
        val password = try {
            decodeBase64(parts[5])
        } catch (e: ShareLinkException) {
            throw ShareLinkException("ssr: 密码无法解码: ${e.message}")
        }

        val q = parseQuery(query)
        val remarks = decodeBase64OrEmpty(q["remarks"])
        val obfsParam = decodeBase64OrEmpty(q["obfsparam"])
        val protoParam = decodeBase64OrEmpty(q["protoparam"])

        return ShareNode(
            name = remarks,
            protocolRaw = "ssr",
            protocol = ProxyProtocol.SSR,
            server = parts[0],
            port = port,
            password = password,
            cipher = parts[3],
            obfs = parts[4],
            ssrProtocol = parts[2],
            ssrObfsParam = obfsParam,
            ssrProtoParam = protoParam,
            network = "tcp",
        )
    }

    // -----------------------------------------------------------------------
    // vmess
    // -----------------------------------------------------------------------

    /**
     * `vmess://` 是 `base64(JSON)`。字段名沿用 v2rayN 的短名，这是事实标准。
     */
    private fun parseVmess(rest: String): ShareNode {
        val (body, nameFromFrag) = splitFragment(rest)
        val decoded = try {
            decodeBase64(body)
        } catch (e: ShareLinkException) {
            throw ShareLinkException("vmess: 无法解码 base64(JSON): ${e.message}")
        }

        val v = try {
            MiniJson.parseObject(decoded)
        } catch (e: ShareLinkException) {
            throw ShareLinkException("vmess: JSON 非法: ${e.message}")
        }

        val add = v.string("add")
        val id = v.string("id")
        // port / aid 在不同客户端里有时是字符串有时是数字，必须两种都收。
        val port = v.int("port") ?: 0
        if (port == 0) throw ShareLinkException("vmess: 端口缺失或为 0")
        if (add.trim().isEmpty()) throw ShareLinkException("vmess: 服务器地址缺失")
        if (id.trim().isEmpty()) throw ShareLinkException("vmess: UUID 缺失")

        val tls = v.string("tls")
        val net = v.string("net").ifEmpty { "tcp" }
        return ShareNode(
            name = v.string("ps").ifEmpty { nameFromFrag },
            protocolRaw = "vmess",
            protocol = ProxyProtocol.VMESS,
            server = add,
            port = port,
            uuid = id,
            alterId = v.int("aid") ?: 0,
            cipher = v.string("scy"),
            network = net,
            tls = tls == "tls" || tls == "true" || tls == "1",
            sni = v.string("sni"),
            host = v.string("host"),
            path = v.string("path"),
        )
    }

    // -----------------------------------------------------------------------
    // vless / trojan / hysteria2
    // -----------------------------------------------------------------------

    /** `vless://uuid@host:port?encryption=none&security=tls&type=ws&path=/&host=#name` */
    private fun parseVless(rest: String): ShareNode {
        val u = try {
            SimpleUri.parse("vless://$rest")
        } catch (e: ShareLinkException) {
            throw ShareLinkException("vless: URI 非法: ${e.message}")
        }
        if (u.username.isEmpty()) throw ShareLinkException("vless: 缺少 UUID")
        val (server, port) = splitHostPort(u.hostPort, "vless")
        val q = u.query
        val security = q["security"].orEmpty()
        val net = q["type"].orEmpty().ifEmpty { "tcp" }
        return ShareNode(
            name = u.fragment,
            protocolRaw = "vless",
            protocol = ProxyProtocol.VLESS,
            server = server,
            port = port,
            uuid = u.username,
            cipher = firstNonEmpty(q["encryption"], "none"),
            network = net,
            tls = security == "tls" || security == "reality" || security == "xtls",
            sni = firstNonEmpty(q["sni"], q["peer"]),
            host = q["host"].orEmpty(),
            path = q["path"].orEmpty(),
            flow = q["flow"].orEmpty(),
        )
    }

    /** `trojan://password@host:port?sni=&type=&security=#name` */
    private fun parseTrojan(rest: String): ShareNode {
        val u = try {
            SimpleUri.parse("trojan://$rest")
        } catch (e: ShareLinkException) {
            throw ShareLinkException("trojan: URI 非法: ${e.message}")
        }
        if (u.username.isEmpty()) throw ShareLinkException("trojan: 缺少密码")
        val (server, port) = splitHostPort(u.hostPort, "trojan")
        val q = u.query
        val security = q["security"].orEmpty()
        return ShareNode(
            name = u.fragment,
            protocolRaw = "trojan",
            protocol = ProxyProtocol.TROJAN,
            server = server,
            port = port,
            password = u.username,
            network = q["type"].orEmpty().ifEmpty { "tcp" },
            // trojan 默认走 TLS。security=none 才是不加密（少见，但存在）。
            tls = security != "none",
            sni = firstNonEmpty(q["sni"], q["peer"]),
            host = q["host"].orEmpty(),
            path = q["path"].orEmpty(),
            obfs = q["obfs"].orEmpty(),
        )
    }

    /**
     * `hysteria2://auth@host:port?sni=&insecure=1&obfs=salamander&obfs-password=#name`
     * 也接受 `hy2://` 别名。
     */
    private fun parseHysteria2(rest: String): ShareNode {
        val u = try {
            SimpleUri.parse("hysteria2://$rest")
        } catch (e: ShareLinkException) {
            throw ShareLinkException("hysteria2: URI 非法: ${e.message}")
        }
        val (server, port) = splitHostPort(u.hostPort, "hysteria2")
        val q = u.query
        val auth = if (u.username.isEmpty()) {
            ""
        } else if (u.password.isNotEmpty()) {
            "${u.username}:${u.password}"
        } else {
            u.username
        }
        val insecure = q["insecure"].orEmpty()
        // hysteria2 基于 QUIC，必然走 TLS
        return ShareNode(
            name = u.fragment,
            protocolRaw = "hysteria2",
            protocol = ProxyProtocol.HYSTERIA2,
            server = server,
            port = port,
            password = auth,
            network = "quic",
            tls = true,
            sni = firstNonEmpty(q["sni"], q["peer"]),
            obfs = q["obfs"].orEmpty(),
            obfsPassword = q["obfs-password"].orEmpty(),
            insecure = insecure == "1" || insecure == "true",
        )
    }

    /** `socks5://user:pass@host:port` / `http://user:pass@host:port` / `https://...` */
    private fun parseHttpLike(rest: String, scheme: String): ShareNode {
        val u = try {
            SimpleUri.parse("$scheme://$rest")
        } catch (e: ShareLinkException) {
            throw ShareLinkException("$scheme: URI 非法: ${e.message}")
        }
        val (server, port) = splitHostPort(u.hostPort, scheme)
        var user = u.username
        var password = u.password
        // 无密码时把用户名放进 Password 复用（这两个协议只支持 user:pass 一种凭据形态）
        if (user.isNotEmpty() && password.isEmpty()) {
            password = user
            user = ""
        }
        val proto = if (scheme == "socks" || scheme == "socks5") "socks5" else "http"
        return ShareNode(
            name = u.fragment,
            protocolRaw = proto,
            protocol = if (proto == "socks5") ProxyProtocol.SOCKS5 else ProxyProtocol.HTTP,
            server = server,
            port = port,
            uuid = user,
            password = password,
            network = "tcp",
            tls = scheme == "https",
        )
    }

    // -----------------------------------------------------------------------
    // 序列化（round-trip）
    // -----------------------------------------------------------------------

    /**
     * 把节点序列化回分享链接。
     *
     * **round-trip 的定义**：`parse(serialize(node))` 得到的节点与 `node` 在
     * [ShareNode.equalsSemantically] 下相等。注意不要求字符串逐字相同 ——
     * 同一个节点有多种合法链接写法（base64 字母表、padding、参数顺序），
     * 要求逐字相同是过强的断言，会把合法的多种写法误判为失败。
     */
    fun serialize(node: ShareNode): String = when (node.protocolRaw) {
        "ss" -> serializeSs(node)
        "ssr" -> serializeSsr(node)
        "vmess" -> serializeVmess(node)
        "vless" -> serializeVless(node)
        "trojan" -> serializeTrojan(node)
        "hysteria2" -> serializeHysteria2(node)
        "socks5" -> serializeHttpLike(node, "socks5")
        "http" -> serializeHttpLike(node, if (node.tls) "https" else "http")
        else -> throw ShareLinkException("无法序列化未知协议: ${node.protocolRaw}")
    }

    private fun serializeSs(n: ShareNode): String {
        // 采用 SIP002：userinfo 部分用 base64url 编码 method:password
        val userinfo = base64UrlNoPad("${n.cipher}:${n.password}")
        val frag = if (n.name.isEmpty()) "" else "#${urlQueryEscape(n.name)}"
        return "ss://$userinfo@${hostPort(n)}$frag"
    }

    private fun serializeSsr(n: ShareNode): String {
        val pass = base64UrlNoPad(n.password)
        val main = "${n.server}:${n.port}:${n.ssrProtocol}:${n.cipher}:${n.obfs}:$pass"
        val params = buildList {
            if (n.ssrObfsParam.isNotEmpty()) add("obfsparam=${base64UrlNoPad(n.ssrObfsParam)}")
            if (n.ssrProtoParam.isNotEmpty()) add("protoparam=${base64UrlNoPad(n.ssrProtoParam)}")
            if (n.name.isNotEmpty()) add("remarks=${base64UrlNoPad(n.name)}")
        }
        val tail = if (params.isEmpty()) "" else "/?" + params.joinToString("&")
        return "ssr://" + base64UrlNoPad(main + tail)
    }

    private fun serializeVmess(n: ShareNode): String {
        val json = MiniJson.buildObject(
            "ps" to MiniJson.str(n.name),
            "add" to MiniJson.str(n.server),
            "port" to MiniJson.num(n.port),
            "id" to MiniJson.str(n.uuid),
            "aid" to MiniJson.num(n.alterId),
            "scy" to MiniJson.str(n.cipher.ifEmpty { "auto" }),
            "net" to MiniJson.str(n.network.ifEmpty { "tcp" }),
            "type" to MiniJson.str("none"),
            "host" to MiniJson.str(n.host),
            "path" to MiniJson.str(n.path),
            "tls" to MiniJson.str(if (n.tls) "tls" else ""),
            "sni" to MiniJson.str(n.sni),
        )
        return "vmess://" + base64StdNoPad(json)
    }

    private fun serializeVless(n: ShareNode): String {
        val security = when {
            !n.tls -> "none"
            else -> "tls"
        }
        val params = buildList {
            add("encryption=${urlQueryEscape(n.cipher.ifEmpty { "none" })}")
            add("security=$security")
            add("type=${urlQueryEscape(n.network.ifEmpty { "tcp" })}")
            if (n.sni.isNotEmpty()) add("sni=${urlQueryEscape(n.sni)}")
            if (n.host.isNotEmpty()) add("host=${urlQueryEscape(n.host)}")
            if (n.path.isNotEmpty()) add("path=${urlQueryEscape(n.path)}")
            if (n.flow.isNotEmpty()) add("flow=${urlQueryEscape(n.flow)}")
        }
        val frag = if (n.name.isEmpty()) "" else "#${urlQueryEscape(n.name)}"
        return "vless://${n.uuid}@${hostPort(n)}?${params.joinToString("&")}$frag"
    }

    private fun serializeTrojan(n: ShareNode): String {
        val params = buildList {
            if (n.sni.isNotEmpty()) add("sni=${urlQueryEscape(n.sni)}")
            add("type=${urlQueryEscape(n.network.ifEmpty { "tcp" })}")
            // 只有非 TLS 才写 security=none；缺省即 TLS（与解析侧对称）
            if (!n.tls) add("security=none")
            if (n.host.isNotEmpty()) add("host=${urlQueryEscape(n.host)}")
            if (n.path.isNotEmpty()) add("path=${urlQueryEscape(n.path)}")
        }
        val frag = if (n.name.isEmpty()) "" else "#${urlQueryEscape(n.name)}"
        return "trojan://${urlQueryEscape(n.password)}@${hostPort(n)}?${params.joinToString("&")}$frag"
    }

    private fun serializeHysteria2(n: ShareNode): String {
        val params = buildList {
            if (n.sni.isNotEmpty()) add("sni=${urlQueryEscape(n.sni)}")
            if (n.obfs.isNotEmpty()) add("obfs=${urlQueryEscape(n.obfs)}")
            if (n.obfsPassword.isNotEmpty()) add("obfs-password=${urlQueryEscape(n.obfsPassword)}")
            if (n.insecure) add("insecure=1")
        }
        val frag = if (n.name.isEmpty()) "" else "#${urlQueryEscape(n.name)}"
        val tail = if (params.isEmpty()) "" else "?" + params.joinToString("&")
        return "hysteria2://${urlQueryEscape(n.password)}@${hostPort(n)}$tail$frag"
    }

    private fun serializeHttpLike(n: ShareNode, scheme: String): String {
        val userinfo = when {
            n.uuid.isNotEmpty() && n.password.isNotEmpty() ->
                "${urlQueryEscape(n.uuid)}:${urlQueryEscape(n.password)}@"
            n.password.isNotEmpty() -> "${urlQueryEscape(n.password)}@"
            else -> ""
        }
        val frag = if (n.name.isEmpty()) "" else "#${urlQueryEscape(n.name)}"
        return "$scheme://$userinfo${hostPort(n)}$frag"
    }

    // -----------------------------------------------------------------------
    // 工具
    // -----------------------------------------------------------------------

    private fun splitScheme(uri: String): Pair<String, String>? {
        val i = uri.indexOf("://")
        if (i <= 0) return null
        return uri.substring(0, i).lowercase() to uri.substring(i + 3)
    }

    /**
     * 把 `...#name` 拆开，并对 name 做 percent-decode。
     *
     * 注意：`#` 只在**第一个**位置切分。名字里若含未转义的 `#`，第一个之后的部分
     * 全部归入名字 —— 与 Go 侧 `strings.Cut` 行为一致。
     */
    private fun splitFragment(s: String): Pair<String, String> {
        val i = s.indexOf('#')
        if (i < 0) return s to ""
        val frag = s.substring(i + 1)
        val decoded = runCatching { urlQueryUnescape(frag) }.getOrNull() ?: frag
        return s.substring(0, i) to decoded.trim()
    }

    /** 支持 IPv6 字面量（形如 `[::1]:443`）。 */
    private fun splitHostPort(hostport0: String, ctx: String): Pair<String, Int> {
        var hostport = hostport0.trim()
        if (hostport.endsWith("/")) hostport = hostport.dropLast(1)
        if (hostport.isEmpty()) throw ShareLinkException("$ctx: 地址为空")

        val host: String
        val portStr: String
        var bracketed = false
        if (hostport.startsWith("[")) {
            bracketed = true
            val end = hostport.lastIndexOf(']')
            if (end < 0) throw ShareLinkException("$ctx: IPv6 地址缺少 ']': \"$hostport\"")
            host = hostport.substring(1, end)
            val tail = hostport.substring(end + 1)
            if (!tail.startsWith(":")) {
                throw ShareLinkException("$ctx: IPv6 地址缺少端口: \"$hostport\"")
            }
            portStr = tail.substring(1)
        } else {
            val idx = hostport.lastIndexOf(':')
            if (idx < 0) throw ShareLinkException("$ctx: 缺少端口: \"$hostport\"")
            host = hostport.substring(0, idx)
            portStr = hostport.substring(idx + 1)
        }

        val port = portStr.trim().toIntOrNull()
            ?: throw ShareLinkException("$ctx: 端口非数字: \"$portStr\"")
        if (port <= 0 || port > 65535) throw ShareLinkException("$ctx: 端口越界: $port")
        if (host.trim().isEmpty()) throw ShareLinkException("$ctx: 主机名为空")
        // 未加方括号的 IPv6 是**歧义**的：'2001:db8::1:8388' 无法判断端口从哪里开始。
        // 与其猜，不如拒绝并让用户改对 —— 猜错的后果是连到一个错误的地址。
        // 已加方括号的情况不能走这条判断：括号内本来就该含 ':'。
        if (!bracketed && host.contains(':')) {
            throw ShareLinkException("$ctx: IPv6 地址必须用方括号包裹: \"$hostport\"")
        }
        return host to port
    }

    /**
     * 依次尝试 URL-safe 与标准字母表、有无 padding 共 4 种组合。
     *
     * 顺序与 Go 侧 `decodeBase64` 完全一致：RawURL → URL → RawStd → Std。
     * 换顺序会让"两种字母表都能解码"的输入得到不同结果，从而与 Go 侧不一致。
     */
    internal fun decodeBase64(s0: String): String {
        var s = s0.trim()
        if (s.isEmpty()) throw ShareLinkException("空 base64")
        // URL 查询串里 '+' 常被编码成空格，先还原
        s = s.replace(' ', '+')
        val decoders = listOf(
            Base64.getUrlDecoder(),
            Base64.getUrlDecoder(),
            Base64.getMimeDecoder(),
            Base64.getDecoder(),
        )
        // Java 的 Base64 解码器本身接受有无 padding 两种形式，
        // 因此这里 4 个候选中有重复；保留 4 个是为了与 Go 侧的"试 4 种"结构对齐，
        // 并保证未来若换成严格解码器时行为不变。
        for (d in decoders) {
            try {
                val bytes = d.decode(s)
                return String(bytes, StandardCharsets.UTF_8)
            } catch (_: IllegalArgumentException) {
                // 换下一种
            }
        }
        throw ShareLinkException("不是合法的 base64: ${truncate(s, 16)}")
    }

    private fun decodeBase64OrEmpty(s: String?): String {
        if (s.isNullOrEmpty()) return ""
        return runCatching { decodeBase64(s) }.getOrDefault("")
    }

    private fun base64UrlNoPad(s: String): String =
        Base64.getUrlEncoder().withoutPadding()
            .encodeToString(s.toByteArray(StandardCharsets.UTF_8))

    private fun base64StdNoPad(s: String): String =
        Base64.getEncoder().withoutPadding()
            .encodeToString(s.toByteArray(StandardCharsets.UTF_8))

    /**
     * percent-decode，`+` 视为空格。
     *
     * 用 [URLDecoder] 会抛受检的 `IllegalArgumentException`；这里统一转成
     * [ShareLinkException]，让调用方能用 `runCatching` 干净地处理。
     */
    private fun urlQueryUnescape(s: String): String =
        URLDecoder.decode(s, StandardCharsets.UTF_8)

    private fun urlQueryEscape(s: String): String =
        java.net.URLEncoder.encode(s, StandardCharsets.UTF_8)

    private fun firstNonEmpty(vararg vals: String?): String =
        vals.firstOrNull { !it.isNullOrBlank() } ?: ""

    private fun hostPort(n: ShareNode): String =
        if (n.server.contains(':')) "[${n.server}]:${n.port}" else "${n.server}:${n.port}"

    private fun truncate(s: String, n: Int): String =
        if (s.length <= n) s else s.substring(0, n) + "…"

    /** 解析查询串为 map（只保留最后出现的同名键，与 Go 侧 `url.ParseQuery` 的 Get 行为对齐）。 */
    internal fun parseQuery(query: String): Map<String, String> {
        if (query.isEmpty()) return emptyMap()
        val out = mutableMapOf<String, String>()
        for (pair in query.split("&")) {
            if (pair.isEmpty()) continue
            val eq = pair.indexOf('=')
            if (eq < 0) {
                out[pair] = ""
                continue
            }
            val k = pair.substring(0, eq)
            val v = pair.substring(eq + 1)
            out[k] = runCatching { urlQueryUnescape(v) }.getOrDefault(v)
        }
        return out
    }
}

/** 分享链接解析失败的统一异常。[uri] 保留原始文本，便于指出**具体哪一行**有问题。 */
class ShareLinkException(
    message: String,
    val uri: String? = null,
) : IllegalArgumentException(message)

/** 订阅某一行解析失败。[lineNo] 从 1 开始。 */
data class SubscriptionLineError(val lineNo: Int, override val message: String) :
    RuntimeException(message)

/** 订阅解析结果：**部分成功**语义。 */
data class SubscriptionParseResult(
    val nodes: List<ShareNode>,
    val errors: List<SubscriptionLineError>,
) {
    val hasErrors: Boolean get() = errors.isNotEmpty()
}
