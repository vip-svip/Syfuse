package io.github.vipsvip.syfuse.domain.node

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test
import java.nio.charset.StandardCharsets
import java.util.Base64

/**
 * 分享链接解析与 round-trip 测试（8 协议）。
 *
 * ## 这是**跨语言契约测试**
 *
 * 每条用例都对应 `src/syfuse/node_test.go` 里的一条 Go 用例（该文件 28 个 `Test*`）。
 * 断言语义刻意保持一一对应，而不是各写各的 —— 两侧只有在**同一组输入上得到同一组结论**
 * 时，消费者才能确信"App 显示的节点"与"内核实际使用的节点"是同一个。
 *
 * 因此本文件里的链接字符串，凡是能用 [b64url] / [b64std] 生成的，都与 Go 侧同样生成，
 * 不写死 base64 字面量 —— 写死会让两侧测试夹具分叉，而夹具分叉了契约就名存实亡。
 */
class ShareLinkTest {

    private val uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"

    // -----------------------------------------------------------------------
    // ss（对应 Go: TestParseSS_* 共 4 条）
    // -----------------------------------------------------------------------

    @Test
    fun `ss SIP002 base64 的 userinfo`() {
        // 对应 Go: TestParseSS_SIP002_Base64Userinfo
        val uri = "ss://${b64url("aes-256-gcm:secret pw")}@1.2.3.4:8388#%E8%8A%82%E7%82%B9A"
        val n = ShareLink.parse(uri)

        assertThat(n.protocolRaw).isEqualTo("ss")
        assertThat(n.server).isEqualTo("1.2.3.4")
        assertThat(n.port).isEqualTo(8388)
        assertThat(n.cipher).isEqualTo("aes-256-gcm")
        assertThat(n.password).isEqualTo("secret pw")
        // percent-decode 后的中文
        assertThat(n.name).isEqualTo("节点A")
    }

    @Test
    fun `ss legacy 全 base64 —— 密码含 @ 必须按最后一个 @ 切分`() {
        // 对应 Go: TestParseSS_LegacyFullBase64
        val uri = "ss://${b64std("aes-128-gcm:p@ss@2.2.2.2:443")}#legacy"
        val n = ShareLink.parse(uri)

        assertThat(n.server).isEqualTo("2.2.2.2")
        assertThat(n.port).isEqualTo(443)
        // 关键：按最后一个 '@' 切分，否则密码会被截断成 "p"
        assertThat(n.password).isEqualTo("p@ss")
        assertThat(n.cipher).isEqualTo("aes-128-gcm")
    }

    @Test
    fun `ss 明文 userinfo 且被 percent-encode`() {
        // 对应 Go: TestParseSS_PlainUserinfo_PercentEncoded
        val n = ShareLink.parse("ss://chacha20-ietf-poly1305:sec%40ret%3A1@3.3.3.3:8388#plain")
        assertThat(n.password).isEqualTo("sec@ret:1")
    }

    @Test
    fun `ss 插件查询串被剥离且不影响解析`() {
        // 对应 Go: TestParseSS_WithPluginQueryIsIgnored
        val uri = "ss://${b64url("aes-256-gcm:pw")}@4.4.4.4:8388/?plugin=obfs-local%3Bobfs%3Dhttp#withplugin"
        val n = ShareLink.parse(uri)

        assertThat(n.server).isEqualTo("4.4.4.4")
        assertThat(n.port).isEqualTo(8388)
        assertThat(n.name).isEqualTo("withplugin")
    }

    // -----------------------------------------------------------------------
    // ssr（对应 Go: TestParseSSR）
    // -----------------------------------------------------------------------

    @Test
    fun `ssr 六段式与三个 base64 参数`() {
        val inner = "5.5.5.5:8080:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:" +
            b64url("ssrpass") +
            "/?remarks=${b64url("SSR节点")}" +
            "&obfsparam=${b64url("obfsp")}" +
            "&protoparam=${b64url("protop")}"
        val n = ShareLink.parse("ssr://${b64url(inner)}")

        assertThat(n.protocolRaw).isEqualTo("ssr")
        assertThat(n.server).isEqualTo("5.5.5.5")
        assertThat(n.port).isEqualTo(8080)
        assertThat(n.password).isEqualTo("ssrpass")
        assertThat(n.cipher).isEqualTo("aes-256-cfb")
        assertThat(n.ssrProtocol).isEqualTo("auth_aes128_md5")
        assertThat(n.obfs).isEqualTo("tls1.2_ticket_auth")
        assertThat(n.name).isEqualTo("SSR节点")
        assertThat(n.ssrObfsParam).isEqualTo("obfsp")
        assertThat(n.ssrProtoParam).isEqualTo("protop")
    }

    // -----------------------------------------------------------------------
    // vmess（对应 Go: TestParseVmess_* 共 3 条）
    // -----------------------------------------------------------------------

    @Test
    fun `vmess base64 JSON 且 port 是字符串`() {
        val payload = """{"v":"2","ps":"VMess节点","add":"v2.example.com","port":"443",""" +
            """"id":"$uuid","aid":"0","scy":"auto","net":"ws","type":"none",""" +
            """"host":"v2.example.com","path":"/ws","tls":"tls","sni":"v2.example.com"}"""
        val n = ShareLink.parse("vmess://${b64std(payload)}")

        assertThat(n.protocolRaw).isEqualTo("vmess")
        assertThat(n.server).isEqualTo("v2.example.com")
        assertThat(n.port).isEqualTo(443)
        assertThat(n.uuid).isEqualTo(uuid)
        assertThat(n.network).isEqualTo("ws")
        assertThat(n.path).isEqualTo("/ws")
        assertThat(n.tls).isTrue()
        assertThat(n.sni).isEqualTo("v2.example.com")
        assertThat(n.name).isEqualTo("VMess节点")
    }

    @Test
    fun `vmess port 与 aid 是数字也必须能收`() {
        val payload = """{"v":"2","ps":"num","add":"n.example.com","port":8443,""" +
            """"id":"$uuid","aid":64,"net":"tcp","tls":""}"""
        val n = ShareLink.parse("vmess://${b64std(payload)}")

        assertThat(n.port).isEqualTo(8443)
        assertThat(n.alterId).isEqualTo(64)
        assertThat(n.tls).isFalse()
    }

    @Test
    fun `vmess URL-safe base64 也必须能解`() {
        val payload = """{"v":"2","ps":"urlsafe","add":"u.example.com","port":"443","id":"$uuid","net":"tcp"}"""
        // 不抛异常即为通过（与 Go 侧同断言强度）
        val n = ShareLink.parse("vmess://${b64url(payload)}")
        assertThat(n.server).isEqualTo("u.example.com")
    }

    // -----------------------------------------------------------------------
    // vless（对应 Go: TestParseVless* 共 2 条）
    // -----------------------------------------------------------------------

    @Test
    fun `vless 含 ws 传输与 percent-encoded 中文名`() {
        val uri = "vless://$uuid@v3.example.com:443" +
            "?encryption=none&security=tls&type=ws&path=%2Fws&host=v3.example.com&sni=v3.example.com" +
            "#VLESS%E8%8A%82%E7%82%B9"
        val n = ShareLink.parse(uri)

        assertThat(n.protocolRaw).isEqualTo("vless")
        assertThat(n.uuid).isEqualTo(uuid)
        assertThat(n.server).isEqualTo("v3.example.com")
        assertThat(n.port).isEqualTo(443)
        assertThat(n.tls).isTrue()
        assertThat(n.network).isEqualTo("ws")
        assertThat(n.path).isEqualTo("/ws")
        assertThat(n.name).isEqualTo("VLESS节点")
    }

    @Test
    fun `vless security=reality 应被视为加密传输`() {
        val uri = "vless://$uuid@r.example.com:443" +
            "?security=reality&type=tcp&flow=xtls-rprx-vision&sni=r.example.com#reality"
        val n = ShareLink.parse(uri)

        assertThat(n.tls).isTrue()
        assertThat(n.flow).isEqualTo("xtls-rprx-vision")
    }

    // -----------------------------------------------------------------------
    // trojan（对应 Go: TestParseTrojan* 共 2 条）
    // -----------------------------------------------------------------------

    @Test
    fun `trojan 默认走 TLS 且密码 percent-decode`() {
        val n = ShareLink.parse("trojan://mypass%21@t.example.com:443?sni=t.example.com&type=tcp#T%E8%8A%82%E7%82%B9")

        assertThat(n.protocolRaw).isEqualTo("trojan")
        assertThat(n.password).isEqualTo("mypass!")
        // trojan 默认走 TLS
        assertThat(n.tls).isTrue()
        assertThat(n.sni).isEqualTo("t.example.com")
    }

    @Test
    fun `trojan security=none 时不启用 TLS`() {
        val n = ShareLink.parse("trojan://pw@t2.example.com:8080?security=none&type=tcp#none")
        assertThat(n.tls).isFalse()
    }

    // -----------------------------------------------------------------------
    // hysteria2（对应 Go: TestParseHysteria2* 共 2 条）
    // -----------------------------------------------------------------------

    @Test
    fun `hysteria2 基于 QUIC 恒为 TLS 且带 obfs`() {
        val uri = "hysteria2://authpass@h.example.com:8443" +
            "?sni=h.example.com&insecure=1&obfs=salamander&obfs-password=obfspw#H%E8%8A%82%E7%82%B9"
        val n = ShareLink.parse(uri)

        assertThat(n.protocolRaw).isEqualTo("hysteria2")
        assertThat(n.password).isEqualTo("authpass")
        assertThat(n.network).isEqualTo("quic")
        assertThat(n.tls).isTrue()
        assertThat(n.insecure).isTrue()
        assertThat(n.obfs).isEqualTo("salamander")
        assertThat(n.obfsPassword).isEqualTo("obfspw")
    }

    @Test
    fun `hysteria2 接受 hy2 别名`() {
        val n = ShareLink.parse("hy2://pw@h2.example.com:443?sni=h2.example.com#alias")
        assertThat(n.server).isEqualTo("h2.example.com")
    }

    // -----------------------------------------------------------------------
    // http / socks5（对应 Go: TestParseSocks5 / TestParseHTTP_HTTPS）
    // -----------------------------------------------------------------------

    @Test
    fun `socks5 含用户名密码与中文名`() {
        val n = ShareLink.parse("socks5://user:pass@127.0.0.1:1080#%E6%9C%AC%E5%9C%B0")

        assertThat(n.protocolRaw).isEqualTo("socks5")
        assertThat(n.uuid).isEqualTo("user")
        assertThat(n.password).isEqualTo("pass")
        assertThat(n.name).isEqualTo("本地")
    }

    @Test
    fun `https 归一化为 http 协议但置 TLS`() {
        val n = ShareLink.parse("https://u:p@proxy.example.com:3128#%E4%BB%A3%E7%90%86")

        assertThat(n.protocolRaw).isEqualTo("http")
        assertThat(n.tls).isTrue()
    }

    // -----------------------------------------------------------------------
    // IPv6（对应 Go: TestParseIPv6* 共 2 条）
    // -----------------------------------------------------------------------

    @Test
    fun `IPv6 字面量带方括号`() {
        val n = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@[2001:db8::1]:8388#v6")
        assertThat(n.server).isEqualTo("2001:db8::1")
        assertThat(n.port).isEqualTo(8388)
    }

    @Test
    fun `未加方括号的 IPv6 必须被拒绝`() {
        // 不加方括号是**歧义**的：无法判断端口从哪里开始。猜错的后果是连到错误地址。
        val ex = runCatching {
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@2001:db8::1:8388#bad")
        }.exceptionOrNull()

        assertThat(ex).isInstanceOf(ShareLinkException::class.java)
        assertThat(ex!!.message).contains("方括号")
    }

    // -----------------------------------------------------------------------
    // 错误路径（对应 Go: TestParseNodeURI_Errors 的 13 个子用例）
    // -----------------------------------------------------------------------

    @Test
    fun `错误路径的错误信息必须能定位原因`() {
        val cases = listOf(
            Triple("空字符串", "", "空 URI"),
            Triple("缺少 scheme", "1.2.3.4:8388", "缺少 scheme"),
            Triple("未知协议", "quantum://x@h:1", "不支持的协议"),
            Triple("ss 缺端口", "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4", "缺少端口"),
            Triple("ss 端口越界", "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:99999", "端口越界"),
            Triple("ss 端口非数字", "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:abc", "端口非数字"),
            Triple("ss 凭据缺冒号", "ss://${b64url("nocolon")}@1.2.3.4:8388", "凭据格式"),
            Triple("vmess 非 JSON", "vmess://${b64std("not json")}", "JSON 非法"),
            Triple("vmess 缺 UUID", "vmess://${b64std("""{"add":"a.com","port":"443"}""")}", "UUID 缺失"),
            Triple("vmess 缺端口", "vmess://${b64std("""{"add":"a.com","id":"$uuid"}""")}", "端口缺失"),
            Triple("vmess 缺服务器", "vmess://${b64std("""{"port":"443","id":"$uuid"}""")}", "服务器地址缺失"),
            Triple("vless 缺 UUID", "vless://@h.com:443", "缺少 UUID"),
            Triple("trojan 缺密码", "trojan://@h.com:443", "缺少密码"),
        )

        for ((label, uri, want) in cases) {
            val ex = runCatching { ShareLink.parse(uri) }.exceptionOrNull()
            assertThat(ex).isNotNull()
            assertThat(ex!!.message).contains(want)
        }
    }

    @Test
    fun `缺省名称按 协议 + 主机 + 端口 生成`() {
        val n = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@9.9.9.9:8388")
        assertThat(n.name).isEqualTo("ss-9.9.9.9:8388")
    }

    // -----------------------------------------------------------------------
    // 订阅解析（对应 Go: TestParseSubscription_* 共 3 条）
    // -----------------------------------------------------------------------

    @Test
    fun `订阅部分成功 —— 坏行不能让整份订阅失败`() {
        val content = listOf(
            "# 这是注释，应被忽略",
            "",
            "ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#ok1",
            "这不是一个合法的节点",
            "ss://${b64url("aes-256-gcm:pw2")}@2.2.2.2:8388#ok2",
            "vmess://${b64std("{坏 JSON}")}",
            "ss://${b64url("aes-256-gcm:pw3")}@3.3.3.3:8388#ok3",
        ).joinToString("\n")

        val (nodes, errors) = ShareLink.parseSubscription(content)

        assertThat(nodes).hasSize(3)
        assertThat(errors).hasSize(2)
        // 关键语义：坏行不能让整份订阅失败
        assertThat(nodes).isNotEmpty()
        // 错误信息必须能定位到行号
        for (e in errors) {
            assertThat(e.message).contains("行")
            assertThat(e.lineNo).isGreaterThan(0)
        }
    }

    @Test
    fun `订阅去重 —— 名字不同应去重，密码不同应保留`() {
        val line = "ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#nameA"
        val sameKey = "ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#nameB"
        val diffPassword = "ss://${b64url("aes-256-gcm:pw2")}@1.1.1.1:8388#nameC"

        val (nodes, errors) = ShareLink.parseSubscription(listOf(line, sameKey, diffPassword).joinToString("\n"))

        assertThat(errors).isEmpty()
        assertThat(nodes).hasSize(2)
    }

    @Test
    fun `整体 base64 包裹的订阅也要能解`() {
        val inner = listOf(
            "ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#a",
            "ss://${b64url("aes-256-gcm:pw2")}@2.2.2.2:8388#b",
        ).joinToString("\n")

        val (nodes, errors) = ShareLink.parseSubscription(b64std(inner))

        assertThat(errors).isEmpty()
        assertThat(nodes).hasSize(2)
    }

    // -----------------------------------------------------------------------
    // round-trip 矩阵 —— 全 8 协议
    // -----------------------------------------------------------------------

    /**
     * 全 8 协议的序列化 → 再解析矩阵。
     *
     * 这是 T4c 的核心断言：**每个协议都必须能走完一圈而不丢字段**。
     * 判据是 [ShareNode.equalsSemantically]（不比较 name/raw，理由见该方法注释）。
     */
    @Test
    fun `八协议 round-trip 矩阵 —— 逐字段等值`() {
        val cases = mapOf(
            "ss" to ShareLink.parse("ss://${b64url("aes-256-gcm:rp@ss:1")}@1.1.1.1:8388#ss"),
            "ssr" to ShareLink.parse(
                "ssr://${b64url(
                    "5.5.5.5:8080:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:" +
                        b64url("ssrpass") +
                        "/?remarks=${b64url("n")}&obfsparam=${b64url("o")}&protoparam=${b64url("p")}",
                )}",
            ),
            "vmess" to ShareLink.parse(
                "vmess://${b64std(
                    """{"ps":"v","add":"v.example.com","port":"443","id":"$uuid",""" +
                        """"aid":"4","scy":"auto","net":"ws","host":"v.example.com",""" +
                        """"path":"/ws","tls":"tls","sni":"v.example.com"}""",
                )}",
            ),
            "vless" to ShareLink.parse(
                "vless://$uuid@v3.example.com:443" +
                    "?encryption=none&security=tls&type=ws&path=%2Fws&host=v3.example.com" +
                    "&sni=v3.example.com&flow=xtls-rprx-vision#vless",
            ),
            "trojan" to ShareLink.parse("trojan://tr%40pass@t.example.com:443?sni=t.example.com&type=ws&path=%2Fx#trojan"),
            "hysteria2" to ShareLink.parse(
                "hysteria2://auth@h.example.com:8443?sni=h.example.com&obfs=salamander&obfs-password=opw#hy2",
            ),
            "http" to ShareLink.parse("http://user:pass@proxy.example.com:3128#http"),
            "socks5" to ShareLink.parse("socks5://user:pass@127.0.0.1:1080#socks"),
        )

        // 覆盖性自检：矩阵必须真的有 8 个协议，且与 SUPPORTED_PROTOCOLS 集合一致。
        // 这条断言防止未来新增协议时忘了扩矩阵（否则矩阵会静默"变小"）。
        assertThat(cases.keys).containsExactlyElementsIn(ShareLink.SUPPORTED_PROTOCOLS.toSet())

        for ((label, original) in cases) {
            val link = ShareLink.serialize(original)
            val reparsed = ShareLink.parse(link)

            val diff = reparsed.diffFrom(original)
            assertThat(diff).isEmpty()
            assertThat(reparsed.equalsSemantically(original)).isTrue()

            // 再走一圈，确保序列化是"幂等"的（第二轮链接应与第一轮相同）
            val link2 = ShareLink.serialize(reparsed)
            assertThat(link2).isEqualTo(link)
        }
    }

    @Test
    fun `序列化结果不含未转义的中文与特殊字符`() {
        // 中文名走 percent-encoding；含 '@' 的密码不能直接拼进 userinfo
        val n = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#%E9%A6%99%E6%B8%AF")
        val link = ShareLink.serialize(n)

        assertThat(link).doesNotContain("香港")
        // 再解析回来仍然是中文
        assertThat(ShareLink.parse(link).name).isEqualTo("香港")
    }

    // -----------------------------------------------------------------------
    // key() 与安全属性
    // -----------------------------------------------------------------------

    @Test
    fun `key 不含名称 —— 同名不同参数是两个节点、同参数不同名是一个节点`() {
        val a = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#nameA")
        val b = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#nameB")
        val c = ShareLink.parse("ss://${b64url("aes-256-gcm:pw2")}@1.1.1.1:8388#nameC")

        assertThat(a.key()).isEqualTo(b.key())
        assertThat(a.key()).isNotEqualTo(c.key())
        assertThat(a.key()).doesNotContain("nameA")
    }

    @Test
    fun `redacted 不泄漏口令与 UUID`() {
        val n = ShareLink.parse("vless://$uuid@v.example.com:443?security=tls#x")
        val r = n.redacted()

        assertThat(r.uuid).isEmpty()
        assertThat(r.password).isEmpty()
        assertThat(r.raw).isEmpty()
        // 但连接信息仍在（UI 要能显示）
        assertThat(r.server).isEqualTo("v.example.com")
        assertThat(r.port).isEqualTo(443)
    }

    @Test
    fun `支持协议清单与实际解析器一致`() {
        // 对应 Go: TestSupportedProtocols_MatchesParsers —— 清单里每个协议都要真能解析
        for (proto in ShareLink.SUPPORTED_PROTOCOLS) {
            val sample = when (proto) {
                "ss" -> "ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388"
                "ssr" -> "ssr://${b64url("1.1.1.1:8388:p:m:o:${b64url("pw")}")}"
                "vmess" -> "vmess://${b64std("""{"add":"a.com","port":"443","id":"$uuid"}""")}"
                "vless" -> "vless://$uuid@a.com:443?security=tls"
                "trojan" -> "trojan://pw@a.com:443"
                "hysteria2" -> "hysteria2://pw@a.com:443"
                "http" -> "http://u:p@a.com:3128"
                "socks5" -> "socks5://u:p@a.com:1080"
                else -> error("清单里有未覆盖的协议: $proto")
            }
            val n = ShareLink.parse(sample)
            assertThat(n.protocolRaw).isEqualTo(proto)
        }
    }
}

// ---------------------------------------------------------------------------
// 与 Go 侧测试夹具同构的编码助手
// ---------------------------------------------------------------------------

/** URL-safe 无 padding base64，对应 Go 测试里的 `b64url`。 */
private fun b64url(s: String): String =
    Base64.getUrlEncoder().withoutPadding().encodeToString(s.toByteArray(StandardCharsets.UTF_8))

/** 标准无 padding base64，对应 Go 测试里的 `b64std`。 */
private fun b64std(s: String): String =
    Base64.getEncoder().withoutPadding().encodeToString(s.toByteArray(StandardCharsets.UTF_8))
