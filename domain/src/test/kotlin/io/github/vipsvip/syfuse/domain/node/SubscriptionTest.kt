package io.github.vipsvip.syfuse.domain.node

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import org.junit.jupiter.api.Test
import java.nio.charset.StandardCharsets
import java.util.Base64

/**
 * 订阅 diff 与订阅头解析测试（T5）。
 *
 * 重点覆盖三类真实场景：
 *  1. 只改名的节点**不应**被报成"删除 + 新增"（否则每次机场调命名都刷一屏噪音）；
 *  2. 凭据轮换应被识别成"改动"，而不是"换了一个节点"；
 *  3. 订阅头字段缺失 / 垃圾值不能让解析整体失败。
 */
class SubscriptionTest {

    private val uuidA = "b831381d-6324-4d53-ad4f-8cda48b30811"
    private val uuidB = "c942492e-7435-5e64-be5f-9deb59c41922"

    // -----------------------------------------------------------------------
    // diff：新增 / 删除
    // -----------------------------------------------------------------------

    @Test
    fun `新增与删除被分别识别`() {
        val old = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#A"),
        )
        val new = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#A"),
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw2")}@2.2.2.2:8388#B"),
        )

        val d = SubscriptionDiffer.diff(old, new)

        assertThat(d.added).hasSize(1)
        assertThat(d.added.single().server).isEqualTo("2.2.2.2")
        assertThat(d.removed).isEmpty()
        assertThat(d.changed).isEmpty()
        assertThat(d.unchangedCount).isEqualTo(1)
        assertThat(d.hasChanges).isTrue()
    }

    @Test
    fun `完全相同时报告无变化`() {
        val nodes = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#A"),
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw2")}@2.2.2.2:8388#B"),
        )

        val d = SubscriptionDiffer.diff(nodes, nodes)

        assertThat(d.hasChanges).isFalse()
        assertThat(d.unchangedCount).isEqualTo(2)
        assertThat(d.summary()).contains("无变化")
    }

    // -----------------------------------------------------------------------
    // diff：改名不应产生噪音
    // -----------------------------------------------------------------------

    @Test
    fun `只改名字应识别为改名而不是删除加新增`() {
        val old = listOf(ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#香港01"))
        val new = listOf(ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#HK-01"))

        val d = SubscriptionDiffer.diff(old, new)

        // 这是本测试的核心：改名**不得**表现为 add + remove
        assertThat(d.added).isEmpty()
        assertThat(d.removed).isEmpty()
        assertThat(d.renamed).hasSize(1)
        assertThat(d.renamed.single().oldName).isEqualTo("香港01")
        assertThat(d.renamed.single().newName).isEqualTo("HK-01")
    }

    // -----------------------------------------------------------------------
    // diff：凭据轮换
    // -----------------------------------------------------------------------

    @Test
    fun `同主机同端口但密码变了应识别为改动`() {
        val old = listOf(ShareLink.parse("ss://${b64url("aes-256-gcm:oldpw")}@1.1.1.1:8388#A"))
        val new = listOf(ShareLink.parse("ss://${b64url("aes-256-gcm:newpw")}@1.1.1.1:8388#A"))

        val d = SubscriptionDiffer.diff(old, new)

        // 弱匹配把"凭据轮换"从 add+remove 里救回来
        assertThat(d.changed).hasSize(1)
        assertThat(d.changed.single().old.password).isEqualTo("oldpw")
        assertThat(d.changed.single().new.password).isEqualTo("newpw")
        assertThat(d.added).isEmpty()
        assertThat(d.removed).isEmpty()
    }

    @Test
    fun `同一前缀出现多个节点时不做弱匹配以避免误判`() {
        // 同主机同端口两个不同节点（不同密码）：弱匹配无法判断谁对谁，应保持 add+remove，
        // 而不是随便配对成"改动"。宁可多报，不可错配。
        val old = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pa")}@1.1.1.1:8388#A"),
            ShareLink.parse("ss://${b64url("aes-256-gcm:pb")}@1.1.1.1:8388#B"),
        )
        val new = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pc")}@1.1.1.1:8388#C"),
            ShareLink.parse("ss://${b64url("aes-256-gcm:pd")}@1.1.1.1:8388#D"),
        )

        val d = SubscriptionDiffer.diff(old, new)

        // 前缀分组各含 2 个，弱匹配应放弃
        assertThat(d.changed).isEmpty()
        assertThat(d.added).hasSize(2)
        assertThat(d.removed).hasSize(2)
    }

    @Test
    fun `摘要包含各类计数`() {
        val old = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#A"),
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw2")}@2.2.2.2:8388#B"),
        )
        val new = listOf(
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw1")}@1.1.1.1:8388#A"),
            ShareLink.parse("ss://${b64url("aes-256-gcm:pw3")}@3.3.3.3:8388#C"),
        )

        val s = SubscriptionDiffer.diff(old, new).summary()
        assertThat(s).contains("新增 1")
        assertThat(s).contains("删除 1")
    }

    // -----------------------------------------------------------------------
    // 订阅头解析
    // -----------------------------------------------------------------------

    @Test
    fun `订阅头完整字段被正确解析`() {
        val info = NodeMapping.parseSubscriptionUserInfo(
            "upload=1234; download=5678; total=100000; expire=1735689600",
        )

        assertThat(info.uploadBytes).isEqualTo(1234L)
        assertThat(info.downloadBytes).isEqualTo(5678L)
        assertThat(info.totalBytes).isEqualTo(100000L)
        assertThat(info.expireAtEpochSeconds).isEqualTo(1735689600L)
        assertThat(info.rawTitle).isNotEmpty()
    }

    @Test
    fun `订阅头字段缺失时留空而不是失败`() {
        val info = NodeMapping.parseSubscriptionUserInfo("total=100000")

        assertThat(info.totalBytes).isEqualTo(100000L)
        assertThat(info.uploadBytes).isNull()
        assertThat(info.downloadBytes).isNull()
        assertThat(info.expireAtEpochSeconds).isNull()
    }

    @Test
    fun `订阅头含垃圾值时忽略该字段`() {
        val info = NodeMapping.parseSubscriptionUserInfo("upload=abc; total=100; expire=; =5; nonsense")

        assertThat(info.uploadBytes).isNull()
        assertThat(info.totalBytes).isEqualTo(100L)
        assertThat(info.expireAtEpochSeconds).isNull()
    }

    @Test
    fun `expire 为零或负表示永不过期`() {
        // 直接显示 1970 年会让用户以为订阅已过期
        assertThat(NodeMapping.parseSubscriptionUserInfo("expire=0").expireAtEpochSeconds).isNull()
        assertThat(NodeMapping.parseSubscriptionUserInfo("expire=-1").expireAtEpochSeconds).isNull()
    }

    @Test
    fun `空订阅头返回空信息而不是抛异常`() {
        assertThat(NodeMapping.parseSubscriptionUserInfo(null).totalBytes).isNull()
        assertThat(NodeMapping.parseSubscriptionUserInfo("").totalBytes).isNull()
        assertThat(NodeMapping.parseSubscriptionUserInfo("   ").totalBytes).isNull()
    }

    // -----------------------------------------------------------------------
    // 映射到领域模型
    // -----------------------------------------------------------------------

    @Test
    fun `映射到领域模型时协议与传输层正确`() {
        val n = ShareLink.parse(
            "vless://$uuidA@v.example.com:443?security=tls&type=ws&path=%2Fws&host=v.example.com" +
                "&sni=v.example.com&flow=xtls-rprx-vision#vless",
        )
        val p = NodeMapping.toProxyNode(n, id = "n1", credentials = null, sortIndex = 2)

        assertThat(p.id).isEqualTo("n1")
        assertThat(p.protocol).isEqualTo(ProxyProtocol.VLESS)
        assertThat(p.server).isEqualTo("v.example.com")
        assertThat(p.port).isEqualTo(443)
        assertThat(p.sortIndex).isEqualTo(2)
        assertThat(p.tls).isNotNull()
        assertThat(p.tls!!.sni).isEqualTo("v.example.com")
        assertThat(p.transport).isNotNull()
        assertThat(p.transport!!.path).isEqualTo("/ws")
    }

    @Test
    fun `凭据不落进领域模型明文 —— 只留引用`() {
        val n = ShareLink.parse("ss://${b64url("aes-256-gcm:SUPER_SECRET")}@1.1.1.1:8388#x")
        val p = NodeMapping.toProxyNode(
            n,
            id = "n1",
            credentials = NodeMapping.CredentialSink { key, secret ->
                // 真正的实现会把它写进加密容器并返回引用
                "vault:${key.hashCode()}:${secret.length}"
            },
        )

        // credentialRef 是引用，不是明文
        assertThat(p.credentialRef).isNotNull()
        assertThat(p.credentialRef).doesNotContain("SUPER_SECRET")
        // rawJson 必须是空的 —— 原始链接含凭据，绝不能随节点落库
        assertThat(p.rawJson).isEmpty()
    }

    @Test
    fun `不传凭据容器时引用为空但映射仍成功`() {
        val n = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#x")
        val p = NodeMapping.toProxyNode(n, id = "n1")

        assertThat(p.credentialRef).isNull()
        assertThat(p.server).isEqualTo("1.1.1.1")
    }

    @Test
    fun `未知传输类型显式报错而不是静默降级`() {
        // 静默降级成 TCP 会连错协议且难以察觉
        val ex = runCatching {
            NodeMapping.toProxyNode(
                ShareNode(protocolRaw = "vless", server = "a.invalid", port = 443, network = "quantum"),
                id = "x",
            )
        }.exceptionOrNull()

        assertThat(ex).isInstanceOf(ShareLinkException::class.java)
        assertThat(ex!!.message).contains("传输类型")
    }

    @Test
    fun `映射保留名称且缺名时回落到标签`() {
        val named = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#%E9%A6%99%E6%B8%AF")
        assertThat(NodeMapping.toProxyNode(named, "n1").name).isEqualTo("香港")

        // 无名字时会走缺省名生成（协议-主机:端口），再映射依然是这个名
        val auto = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@2.2.2.2:8388")
        assertThat(NodeMapping.toProxyNode(auto, "n2").name).isEqualTo("ss-2.2.2.2:8388")
    }

    @Test
    fun `八协议都能映射到领域模型`() {
        val samples = mapOf(
            "ss" to "ss://${b64url("aes-256-gcm:pw")}@1.1.1.1:8388#a",
            "ssr" to "ssr://${b64url("1.1.1.1:8388:p:m:o:${b64url("pw")}")}#b",
            "vmess" to "vmess://${b64std("""{"add":"a.com","port":"443","id":"$uuidA"}""")}#c",
            "vless" to "vless://$uuidB@a.com:443?security=tls#d",
            "trojan" to "trojan://pw@a.com:443#e",
            "hysteria2" to "hysteria2://pw@a.com:443#f",
            "http" to "http://u:p@a.com:3128#g",
            "socks5" to "socks5://u:p@a.com:1080#h",
        )

        assertThat(samples.keys).containsExactlyElementsIn(ShareLink.SUPPORTED_PROTOCOLS.toSet())
        for ((label, link) in samples) {
            val p = NodeMapping.toProxyNode(ShareLink.parse(link), id = "n-$label")
            assertThat(p.protocol).isNotNull()
            assertThat(p.server).isNotEmpty()
            assertThat(p.port).isGreaterThan(0)
        }
    }
}

private fun b64url(s: String): String =
    Base64.getUrlEncoder().withoutPadding().encodeToString(s.toByteArray(StandardCharsets.UTF_8))

private fun b64std(s: String): String =
    Base64.getEncoder().withoutPadding().encodeToString(s.toByteArray(StandardCharsets.UTF_8))
