package io.github.vipsvip.syfuse.domain.node

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import io.github.vipsvip.syfuse.domain.model.TransportType
import org.junit.jupiter.api.Test

/**
 * `ShareNode → ProxyNode` 映射测试。
 *
 * 这些用例守的是一件事：**导入订阅时不能丢字段、不能泄漏明文凭据**。
 * 前者丢字段的症状很隐蔽（节点能连上但行为不对，比如 SNI 没了、obfs 密码没了），
 * 后者更危险（明文口令落库）。
 *
 * 因此断言分两类：
 *  ① 逐字段保真 —— 传输层、TLS、插件、端口、服务器必须原样带到 [ProxyNode]；
 *  ② 凭据隔离 —— 明文口令/UUID **不得**出现在 `credentialRef`，除非调用方显式传入引用。
 */
class ShareNodeMappingTest {

    // -----------------------------------------------------------------------
    // 凭据隔离（R5）
    // -----------------------------------------------------------------------

    @Test
    fun `默认不写入任何凭据引用`() {
        val node = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388").toProxyNode("id1")

        // 关键断言：默认 credentialRef 为 null。
        // 若哪天有人"顺手"把明文塞进来，这条会立刻失败。
        assertThat(node.credentialRef).isNull()
    }

    @Test
    fun `明文口令不进入结构化字段`() {
        val node = ShareLink.parse("trojan://secretpw@t.example.invalid:443").toProxyNode("id1")

        // ProxyNode 里没有任何"明文口令"字段；这里断言所有字符串字段都不含它。
        val structural = listOf(node.id, node.name, node.server, node.rawJson)
        for (f in structural) {
            assertThat(f).doesNotContain("secretpw")
        }
    }

    @Test
    fun `显式传入的凭据引用被原样保留`() {
        val node = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388")
            .toProxyNode("id1", credentialRef = "vault://node/id1")

        assertThat(node.credentialRef).isEqualTo("vault://node/id1")
    }

    // -----------------------------------------------------------------------
    // 逐字段保真
    // -----------------------------------------------------------------------

    @Test
    fun `服务器端口协议与名称原样带过`() {
        val sn = ShareLink.parse("vmess://${vmessJson("hk1", "9.9.9.9", 443)}")
        val node = sn.toProxyNode("id-hk1", sortIndex = 7)

        assertThat(node.id).isEqualTo("id-hk1")
        assertThat(node.sortIndex).isEqualTo(7)
        assertThat(node.protocol).isEqualTo(ProxyProtocol.VMESS)
        assertThat(node.server).isEqualTo("9.9.9.9")
        assertThat(node.port).isEqualTo(443)
        assertThat(node.name).isEqualTo("hk1")
    }

    @Test
    fun `名称缺失时用 protocol 与服务器兜底而不是留空`() {
        // 无 fragment → 解析器会给默认名；若它为空，映射层必须再兜一层。
        val sn = ShareLink.parse("trojan://pw@t.example.invalid:443").copy(name = "")
        val node = sn.toProxyNode("id1")

        assertThat(node.name).isNotEmpty()
        assertThat(node.name).contains("t.example.invalid")
    }

    @Test
    fun `ws 传输的 host 与 path 带到 TransportOptions`() {
        val uri = "trojan://pw@t.example.invalid:443?type=ws&host=cdn.example.invalid&path=%2Fws#n"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        assertThat(node.transport).isNotNull()
        assertThat(node.transport!!.type).isEqualTo(TransportType.WS)
        assertThat(node.transport!!.host).isEqualTo("cdn.example.invalid")
        assertThat(node.transport!!.path).isEqualTo("/ws")
    }

    @Test
    fun `grpc 传输被识别`() {
        val uri = "vless://$UUID@v.example.invalid:443?type=grpc&serviceName=gs#n"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        assertThat(node.transport!!.type).isEqualTo(TransportType.GRPC)
    }

    @Test
    fun `普通 tcp 节点不挂空的 transport`() {
        val node = ShareLink.parse("trojan://pw@t.example.invalid:443").toProxyNode("id1")

        // 空 transport 会让"未配置传输"与"配置了但为空"无法区分。
        assertThat(node.transport).isNull()
    }

    @Test
    fun `未启用 TLS 的节点不挂空的 tls 对象`() {
        val node = ShareLink.parse("socks5://1.2.3.4:1080").toProxyNode("id1")

        assertThat(node.tls).isNull()
    }

    @Test
    fun `TLS 节点的 sni 与 insecure 带到 TlsOptions`() {
        // 注意参数名：hysteria2 用 `insecure=1`（不是 `allowInsecure=1`）。
        // 这个差别是**探针测出来的** —— 初版我用 allowInsecure=1 写夹具，
        // 测试立刻失败并告诉我解析器读的是 insecure。夹具写错就该改夹具，
        // 而不是把解析器改成迎合夹具。
        val uri = "hysteria2://pw@h.example.invalid:443?sni=sni.example.invalid&insecure=1#n"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        assertThat(node.tls).isNotNull()
        assertThat(node.tls!!.sni).isEqualTo("sni.example.invalid")
        assertThat(node.tls!!.allowInsecure).isTrue()
        assertThat(node.tls!!.skipCertVerify).isTrue()
    }

    @Test
    fun `sni 为空时不拿 host 顶替`() {
        // 两者语义不同：host 是 HTTP 头，sni 是握手扩展。顶替会让服务端看到错误的握手。
        val uri = "trojan://pw@t.example.invalid:443?security=tls&host=cdn.example.invalid#n"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        assertThat(node.tls).isNotNull()
        assertThat(node.tls!!.sni).isNull()
    }

    @Test
    fun `ss 的 obfs 与 obfs-password 同时进 PluginOptions`() {
        // 两个参数必须同时生效：只带一个等于配错。
        // 参数名是 `obfs` / `obfs-password`（与 hysteria2 同源），
        // 不是 SIP002 的 `plugin=obfs-local;obfs=http` 写法 —— 后者解析器不认，
        // 这是探针实测结论（初版夹具用 plugin= 形式，测试失败后改的）。
        val uri = "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388?obfs=http&obfs-password=obfspw#n"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        assertThat(node.plugin).isNotNull()
        assertThat(node.plugin!!.name).isEqualTo("http")
        assertThat(node.plugin!!.opts["obfs-password"]).isEqualTo("obfspw")
    }

    @Test
    fun `无 obfs 的节点不挂 plugin`() {
        val node = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388").toProxyNode("id1")

        assertThat(node.plugin).isNull()
    }

    @Test
    fun `rawJson 必须为空 —— 原始链接含明文凭据`() {
        // **这是本文件最重要的一条断言。**
        // ProxyNode.rawJson 会直接落进 Room 的普通文本列，而原始分享链接里含明文口令。
        // 一旦把它写进去，CredentialVault 与 R5（凭据不落明文）就整条作废。
        // 我在初版实现里正是这么写的（"留痕方便排障"），被 SubscriptionTest 的
        // 既有断言拦下 —— 那条断言是对的，我改了实现而不是改它。
        val uri = "trojan://SUPER_SECRET_PW@t.example.invalid:443#%E8%8A%82%E7%82%B9"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        assertThat(node.rawJson).isEmpty()
        assertThat(node.rawJson).doesNotContain("SUPER_SECRET_PW")
    }

    @Test
    fun `任何字符串字段都不得包含明文凭据`() {
        // 全字段扫描而不是只查 credentialRef：
        // 缺陷往往出现在"没人想到要检查"的字段上（我就是栽在 rawJson）。
        val uri = "vless://$UUID@v.example.invalid:443?security=tls#n"
        val node = ShareLink.parse(uri).toProxyNode("id1")

        val fields = listOf(
            node.id, node.name, node.server, node.credentialRef.orEmpty(), node.rawJson,
            node.transport?.host.orEmpty(), node.transport?.path.orEmpty(),
            node.tls?.sni.orEmpty(),
        )
        for (f in fields) {
            assertThat(f).doesNotContain(UUID)
        }
    }

    @Test
    fun `八种协议都能映射出正确的枚举`() {
        val cases = mapOf(
            "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388" to ProxyProtocol.SS,
            "trojan://pw@t.example.invalid:443" to ProxyProtocol.TROJAN,
            "vless://$UUID@v.example.invalid:443" to ProxyProtocol.VLESS,
            "hysteria2://pw@h.example.invalid:443" to ProxyProtocol.HYSTERIA2,
            "hy2://pw@h.example.invalid:443" to ProxyProtocol.HYSTERIA2,
            "http://1.2.3.4:8080" to ProxyProtocol.HTTP,
            "socks5://1.2.3.4:1080" to ProxyProtocol.SOCKS5,
        )
        for ((uri, expected) in cases) {
            val node = ShareLink.parse(uri).toProxyNode("id")
            assertThat(node.protocol).isEqualTo(expected)
        }
    }

    @Test
    fun `https 归一化为 HTTP 协议并以 tls 标记加密`() {
        // 解析器的既定设计：`https://` **不**产出 ProxyProtocol.HTTPS，
        // 而是产出 HTTP + tls=true。这样"是否加密"只有 tls 一个真相来源，
        // 不会出现 HTTP/HTTPS 两条分支各自判断加密与否。
        // 初版夹具直接期望 HTTPS，测试失败后我读了 parseHttpLike 的源码确认这是有意为之，
        // 于是把断言改成锁定这个设计，而不是把设计改掉。
        val node = ShareLink.parse("https://1.2.3.4:8443").toProxyNode("id")

        assertThat(node.protocol).isEqualTo(ProxyProtocol.HTTP)
        assertThat(node.tls).isNotNull()
        assertThat(node.tls!!.sni).isNull()
    }

    @Test
    fun `未知协议原文兜底到 SOCKS5 而不是抛异常`() {
        // 与 data 层 enumOr(ProxyProtocol.SOCKS5) 的兜底保持一致，
        // 避免同一份数据在两处得出不同答案。
        val sn = ShareNode(name = "x", protocolRaw = "vendor-private", server = "1.2.3.4", port = 80)
        val node = sn.toProxyNode("id")

        assertThat(node.protocol).isEqualTo(ProxyProtocol.SOCKS5)
    }

    @Test
    fun `未知传输类型兜底到 TCP 而不是抛异常`() {
        // 一条订阅里出现厂商私有传输名是常态，为它整份订阅失败不可接受。
        val sn = ShareNode(
            name = "x", protocolRaw = "trojan", server = "1.2.3.4", port = 443,
            network = "vendor-transport",
        )
        val node = sn.toProxyNode("id")

        // transport 为 null 或 TCP 都可接受，关键是**不抛异常**。
        assertThat(node.transport?.type ?: TransportType.TCP).isEqualTo(TransportType.TCP)
    }

    // -----------------------------------------------------------------------
    // 夹具
    // -----------------------------------------------------------------------

    private fun vmessJson(name: String, host: String, port: Int): String = b64std(
        """{"v":"2","ps":"$name","add":"$host","port":"$port","id":"$UUID","aid":"0","scy":"auto","net":"tcp","type":"none","host":"","path":"","tls":""}"""
    )

    private companion object {
        const val UUID = "b831381d-6324-4d53-ad4f-8cda48b30811"
    }
}

// 与 ShareLinkTest / SubscriptionTest 里的同名辅助保持一致（两边各自 file-private，
// 是有意的：这三个文件各自独立可读，不为了少 4 行而引入共享测试基建）。
private fun b64url(s: String): String =
    java.util.Base64.getUrlEncoder().withoutPadding()
        .encodeToString(s.toByteArray(Charsets.UTF_8))

private fun b64std(s: String): String =
    java.util.Base64.getEncoder().withoutPadding()
        .encodeToString(s.toByteArray(Charsets.UTF_8))
