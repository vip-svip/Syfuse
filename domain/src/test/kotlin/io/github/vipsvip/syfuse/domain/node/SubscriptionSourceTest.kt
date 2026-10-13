package io.github.vipsvip.syfuse.domain.node

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test
import java.util.Base64

/**
 * 订阅统一入口的形态识别测试。
 *
 * 重点盯三类风险：
 *  ① **base64 包裹的 Clash 配置** —— 解码成功了但含 "://" 吗？不含，
 *     于是旧逻辑回落到"每行一个 URI"，得到 0 个节点（真实缺口，非假想）；
 *  ② **Clash 判断不被 base64 抢走** —— 注意：这条靠的是字母表与无换行两个
 *     硬条件，不是靠判断顺序（变异测试已证伪"顺序"是边界，见 SubscriptionSource 类注释）；
 *  ③ **空输入 / 垃圾输入**不炸，而是给出空结果。
 */
class SubscriptionSourceTest {

    // -----------------------------------------------------------------------
    // 形态识别
    // -----------------------------------------------------------------------

    @Test
    fun `Clash 配置被识别为 CLASH_YAML`() {
        val r = SubscriptionSource.parse(
            """
            mixed-port: 7890
            proxies:
              - {name: a, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}
            """.trimIndent()
        )

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.CLASH_YAML)
        assertThat(r.nodes).hasSize(1)
    }

    @Test
    fun `纯分享链接列表被识别为 SHARE_LINKS`() {
        val r = SubscriptionSource.parse(
            "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388#a\n" +
                "trojan://pw@t.example.invalid:443#b\n"
        )

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.SHARE_LINKS)
        assertThat(r.nodes).hasSize(2)
    }

    @Test
    fun `整份 base64 包裹的链接列表被识别为 BASE64_LINKS`() {
        val links = "ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388#a\n" +
            "trojan://pw@t.example.invalid:443#b\n"
        val wrapped = Base64.getEncoder().encodeToString(links.toByteArray(Charsets.UTF_8))

        val r = SubscriptionSource.parse(wrapped)

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.BASE64_LINKS)
        assertThat(r.nodes).hasSize(2)
    }

    @Test
    fun `URL-safe base64 包裹同样识别`() {
        // 部分机场用 base64url；两者都真实存在。
        val links = "trojan://pw@t.example.invalid:443#b\n"
        val wrapped = Base64.getUrlEncoder().encodeToString(links.toByteArray(Charsets.UTF_8))

        val r = SubscriptionSource.parse(wrapped)

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.BASE64_LINKS)
        assertThat(r.nodes).hasSize(1)
    }

    @Test
    fun `base64 包裹的 Clash 配置被识别为 CLASH_YAML`() {
        // 这是**真实缺口**而非假想：Hub 的"通用订阅"会把 Clash 配置整体 base64 一发。
        // 旧实现在解码后直接找 "://"，Clash 的 YAML 键值里没有它，
        // 于是回落到"每行一个 URI"，导入 0 个节点 —— 用户以为订阅坏了。
        val clash = """
            mixed-port: 7890
            proxies:
              - {name: a, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}
              - {name: b, type: trojan, server: b.com, port: 443, password: pw}
        """.trimIndent()
        val wrapped = Base64.getEncoder().encodeToString(clash.toByteArray(Charsets.UTF_8))

        val r = SubscriptionSource.parse(wrapped)

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.CLASH_YAML)
        assertThat(r.nodes).hasSize(2)
        assertThat(r.nodes[0].server).isEqualTo("a.com")
        assertThat(r.nodes[1].server).isEqualTo("b.com")
    }

    @Test
    fun `base64 包裹的空 proxies 段返回空列表且不抛异常`() {
        val wrapped = Base64.getEncoder()
            .encodeToString("proxies: []\n".toByteArray(Charsets.UTF_8))

        val r = SubscriptionSource.parse(wrapped)

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.CLASH_YAML)
        assertThat(r.nodes).isEmpty()
    }

    // -----------------------------------------------------------------------
    // 识别顺序（这是本文件的核心风险）
    // -----------------------------------------------------------------------

    @Test
    fun `Clash 判断不被 base64 抢走 —— 不会把 Clash 当 base64 解成乱码`() {
        // 这条守住的**不是**判断顺序：交换顺序结论不变（已做变异测试）。
        // 它真正守的是 decodeBase64Quietly 的两个硬条件 —— 无换行 + 全字母表。
        // 一份带缩进的 YAML 两者都不满足，所以永远走不到 base64 分支。
        val clash = "proxies:\n  - {name: x, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}\n"

        val r = SubscriptionSource.parse(clash)

        // 必须是 CLASH_YAML；若格式判定被改坏，这条会立刻失败。
        assertThat(r.format).isEqualTo(SubscriptionSource.Format.CLASH_YAML)
        assertThat(r.nodes).hasSize(1)
        assertThat(r.nodes[0].server).isEqualTo("a.com")
    }

    @Test
    fun `含 proxies 段但也含链接的混合内容按 Clash 处理`() {
        // 机场有时在 Clash 配置末尾附加说明性链接。按 Clash 取 proxies 段
        // 能拿到真实节点；而它本来也进不了 base64 分支（含空格与换行）。
        val mixed = """
        proxies:
          - {name: a, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}
        # 备用：trojan://pw@t.example.invalid:443
        """.trimIndent()

        val r = SubscriptionSource.parse(mixed)

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.CLASH_YAML)
        assertThat(r.nodes).hasSize(1)
    }

    // -----------------------------------------------------------------------
    // 容错
    // -----------------------------------------------------------------------

    @Test
    fun `空内容返回空列表而不是抛异常`() {
        assertThat(SubscriptionSource.parse("").nodes).isEmpty()
        assertThat(SubscriptionSource.parse("   \n\n  ").nodes).isEmpty()
    }

    @Test
    fun `垃圾内容返回空列表而不是抛异常`() {
        assertThat(SubscriptionSource.parse("这不是订阅，只是一段话").nodes).isEmpty()
    }

    @Test
    fun `base64 解出非链接内容时回落到分享链接路径而不是报错`() {
        // 一段合法 base64，但解码后不含 "://" —— 那它就不是订阅。
        val notASub = Base64.getEncoder().encodeToString("hello world".toByteArray())

        val r = SubscriptionSource.parse(notASub)

        // 回落到 SHARE_LINKS 后得到 0 节点，且**不抛异常**。
        assertThat(r.nodes).isEmpty()
    }

    @Test
    fun `分享链接路径的部分成功语义被保留`() {
        val r = SubscriptionSource.parse(
            "trojan://pw@t.example.invalid:443#ok\n" +
                "这不是一个链接\n" +
                "trojan://pw@t2.example.invalid:443#ok2\n"
        )

        assertThat(r.nodes).hasSize(2)
        assertThat(r.lineErrors).hasSize(1)
        assertThat(r.summary()).contains("1 行无法解析")
    }

    @Test
    fun `订阅头信息被解析并带出`() {
        val r = SubscriptionSource.parse(
            "trojan://pw@t.example.invalid:443#ok",
            userInfoHeader = "upload=1024; download=2048; total=10737418240; expire=1893456000",
        )

        assertThat(r.info.uploadBytes).isEqualTo(1024)
        assertThat(r.info.downloadBytes).isEqualTo(2048)
        assertThat(r.info.totalBytes).isEqualTo(10737418240L)
        assertThat(r.info.expireAtEpochSeconds).isEqualTo(1893456000L)
    }

    @Test
    fun `摘要含形态与节点数`() {
        val r = SubscriptionSource.parse(
            "proxies:\n  - {name: a, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}\n"
        )

        assertThat(r.summary()).contains("Clash")
        assertThat(r.summary()).contains("1 个节点")
    }

    @Test
    fun `CRLF 的 Clash 配置同样识别`() {
        val r = SubscriptionSource.parse(
            "proxies:\r\n  - {name: a, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}\r\n"
        )

        assertThat(r.format).isEqualTo(SubscriptionSource.Format.CLASH_YAML)
        assertThat(r.nodes).hasSize(1)
    }

    // -----------------------------------------------------------------------

    private fun b64url(s: String): String =
        java.util.Base64.getUrlEncoder().withoutPadding()
            .encodeToString(s.toByteArray(Charsets.UTF_8))
}
