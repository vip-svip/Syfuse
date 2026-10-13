package io.github.vipsvip.syfuse.domain.node

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import org.junit.jupiter.api.Test
import java.util.Base64

/**
 * 订阅导入规划器测试。
 *
 * 这里盯的全是"**静默损坏用户数据**"类的 bug，它们不会崩、不会报错，
 * 只在用户下次刷新订阅后悄悄发生，因此必须在单元测试里钉死：
 *
 *  ① 重复导入不产生重复节点，也**不取消用户的收藏**；
 *  ② 下线节点清理只清理"仅属于本订阅"的节点，不误删别的订阅在用的；
 *  ③ 节点主键里**不含明文口令**（R5：凭据不进普通列，主键也是普通列）；
 *  ④ 坏行（无端口/无服务器）被跳过，不在 UI 里留下幽灵节点。
 */
class SubscriptionImportTest {

    private val subUrl = "https://airport.example.com/subscribe?token=abc"

    private fun ssNode(password: String = "pw", server: String = "a.example.invalid", port: Int = 8388) =
        ShareLink.parse("ss://${b64url("aes-256-gcm:$password")}@$server:$port")

    private fun trojanNode(server: String = "t.example.invalid", password: String = "tpw") =
        ShareLink.parse("trojan://$password@$server:443")

    private fun outcome(vararg nodes: ShareNode, lineErrors: Int = 0) =
        SubscriptionSource.Outcome(
            nodes = nodes.toList(),
            format = SubscriptionSource.Format.SHARE_LINKS,
            lineErrors = List(lineErrors) { SubscriptionLineError(it + 1, "err") },
        )

    // -----------------------------------------------------------------------
    // 主键与凭据隔离（R5）
    // -----------------------------------------------------------------------

    @Test
    fun `节点主键不含明文口令`() {
        val node = ssNode(password = "super-secret-password")

        val id = SubscriptionImport.nodeIdOf(node)

        // 主键会落进 Room 的普通文本列，所以它绝不能含明文口令。
        assertThat(id).doesNotContain("super-secret-password")
        // 但它必须是稳定的：同一条链接两次导入得到同一个 id（去重依赖这一点）。
        assertThat(SubscriptionImport.nodeIdOf(node)).isEqualTo(id)
    }

    @Test
    fun `同服务器同端口不同口令是两个不同节点`() {
        val a = SubscriptionImport.nodeIdOf(ssNode(password = "pw1"))
        val b = SubscriptionImport.nodeIdOf(ssNode(password = "pw2"))
        assertThat(a).isNotEqualTo(b)
    }

    @Test
    fun `不同订阅里的同名同址节点是同一个主键`() {
        // 节点 key 刻意不含 name：同一节点在两份订阅里名字不同，不该算两个节点。
        val fromA = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388#机场A")
        val fromB = ShareLink.parse("ss://${b64url("aes-256-gcm:pw")}@1.2.3.4:8388#机场B")
        assertThat(SubscriptionImport.nodeIdOf(fromA)).isEqualTo(SubscriptionImport.nodeIdOf(fromB))
    }

    // -----------------------------------------------------------------------
    // 新增 / 更新
    // -----------------------------------------------------------------------

    @Test
    fun `首次导入全部计为新增`() {
        val plan = SubscriptionImport.plan(
            outcome(ssNode(), trojanNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(plan.upsert).hasSize(2)
        assertThat(plan.newCount).isEqualTo(2)
        assertThat(plan.updatedCount).isEqualTo(0)
        assertThat(plan.removedCount).isEqualTo(0)
    }

    @Test
    fun `重复导入同一订阅不产生重复节点`() {
        val first = SubscriptionImport.plan(
            outcome(ssNode(), trojanNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        val second = SubscriptionImport.plan(
            outcome(ssNode(), trojanNode()),
            existing = first.upsert,
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(second.upsert).hasSize(2)
        assertThat(second.newCount).isEqualTo(0)
        assertThat(second.updatedCount).isEqualTo(2)
    }

    @Test
    fun `重复导入保留用户的收藏`() {
        // 这是本文件最重要的一条：若实现图省事直接用"新节点"覆盖旧节点，
        // 用户收藏会在下次刷新订阅后静默消失。
        val first = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )
        val favorited = first.upsert[0].copy(favorite = true)

        val second = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = listOf(favorited),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(second.upsert[0].favorite).isTrue()
    }

    @Test
    fun `重复导入保留已测得的延迟`() {
        val first = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )
        val tested = first.upsert[0].copy(latencyMs = 87)

        val second = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = listOf(tested),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(second.upsert[0].latencyMs).isEqualTo(87)
    }

    // -----------------------------------------------------------------------
    // 来源标签与下线清理
    // -----------------------------------------------------------------------

    @Test
    fun `来源标签稳定且随 URL 变化`() {
        assertThat(SubscriptionImport.sourceTag(subUrl)).isEqualTo(SubscriptionImport.sourceTag(subUrl + "  "))
        assertThat(SubscriptionImport.sourceTag(subUrl))
            .isNotEqualTo(SubscriptionImport.sourceTag("https://other.example.com/s"))
        // 手动粘贴没有来源标签，因此永不触发清理。
        assertThat(SubscriptionImport.sourceTag(null)).isNull()
        assertThat(SubscriptionImport.sourceTag("   ")).isNull()
    }

    @Test
    fun `导入的节点带上来源标签`() {
        val plan = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(plan.upsert[0].tags).contains(SubscriptionImport.sourceTag(subUrl))
    }

    @Test
    fun `订阅下线的节点被列入清理`() {
        val first = SubscriptionImport.plan(
            outcome(ssNode(), trojanNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        // 第二次导入只剩一个节点：另一个应被判为下线。
        val second = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = first.upsert,
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(second.staleIds).hasSize(1)
        assertThat(second.removedCount).isEqualTo(1)
        assertThat(second.staleIds).containsExactly(first.upsert[1].id)
    }

    @Test
    fun `共享节点不被误删`() {
        val otherTag = SubscriptionImport.sourceTag("https://other.example.com/s")
        val shared = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = emptyList(),
            sourceTag = otherTag,
            credentials = null,
        ).upsert[0]

        // 同一节点又被本订阅导入 → 它带两个 sub: 标签。
        val mine = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = listOf(shared),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )
        assertThat(mine.upsert[0].tags).containsExactly(otherTag, SubscriptionImport.sourceTag(subUrl))

        // 本订阅下一次刷新时该节点已不在内容里，但另一份订阅仍在用 → **不能删**。
        val later = SubscriptionImport.plan(
            outcome(trojanNode()),
            existing = mine.upsert,
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )
        assertThat(later.staleIds).isEmpty()
    }

    @Test
    fun `手动粘贴不清理任何既有节点`() {
        val existing = SubscriptionImport.plan(
            outcome(ssNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        ).upsert

        val pasted = SubscriptionImport.plan(
            outcome(trojanNode()),
            existing = existing,
            sourceTag = null,
            credentials = null,
        )

        assertThat(pasted.staleIds).isEmpty()
        assertThat(pasted.removedCount).isEqualTo(0)
    }

    @Test
    fun `用户手工添加且无订阅标签的节点永不被清理`() {
        val manual = ProxyNode(
            id = "manual-1", name = "手填", protocol = io.github.vipsvip.syfuse.domain.model.ProxyProtocol.SOCKS5,
            server = "10.0.0.2", port = 1080,
        )

        val plan = SubscriptionImport.plan(
            outcome(trojanNode()),
            existing = listOf(manual),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(plan.staleIds).isEmpty()
    }

    // -----------------------------------------------------------------------
    // 凭据落库
    // -----------------------------------------------------------------------

    @Test
    fun `凭据经 sink 落库并只留引用`() {
        // 假实现刻意**不**回显任何输入：真实实现是 Keystore 加密，
        // 无论 secret 多长，返回值都与明文无字面关系。
        val sink = NodeMapping.CredentialSink { secret -> "v1:enc:${secret.length}" }

        val plan = SubscriptionImport.plan(
            outcome(ssNode(password = "topsecret")),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = sink,
        )

        assertThat(plan.upsert[0].credentialRef).isNotNull()
        assertThat(plan.upsert[0].credentialRef).doesNotContain("topsecret")
    }

    @Test
    fun `无 sink 时凭据引用为空而不是泄漏明文`() {
        val plan = SubscriptionImport.plan(
            outcome(ssNode(password = "topsecret")),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        // 引用为空是**合法中间态**：节点在、凭据未登记，连接时报"缺凭据"。
        // 绝不能反过来把明文塞进节点——那会让 R5 整条作废。
        assertThat(plan.upsert[0].credentialRef).isNull()
        assertThat(plan.upsert[0].rawJson).isEmpty()
    }

    // -----------------------------------------------------------------------
    // 坏行与重名
    // -----------------------------------------------------------------------

    @Test
    fun `无端口或无服务器的行被跳过而不是留下幽灵节点`() {
        // 直接构造 ShareNode：走 ShareLink.parse 的话缺端口会**抛异常**
        // （那是 ShareLink 的部分成功语义，解析层就挡掉了）。
        // 这里要验证的是规划器自己的兜底 —— 万一上游递来坏节点，也不能进库。
        val noPort = ShareNode(protocolRaw = "trojan", server = "t.example.invalid", port = 0)
        val noServer = ShareNode(protocolRaw = "trojan", server = "", port = 443)

        val plan = SubscriptionImport.plan(
            outcome(noPort, noServer, ssNode()),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(plan.upsert).hasSize(1)
        assertThat(plan.upsert[0].server).isEqualTo("a.example.invalid")
    }

    @Test
    fun `同一订阅内重名节点追加序号`() {
        // 机场重名很常见；不去重的话 UI 上一片同名，用户分不清谁是谁。
        // 同服务器同端口、仅口令不同 → displayLabel（协议·服务器:端口）完全相同，
        // 这才是真正触发名字去重的情形。
        val plan = SubscriptionImport.plan(
            outcome(
                ssNode(password = "p1", server = "a.example.invalid", port = 8388),
                ssNode(password = "p2", server = "a.example.invalid", port = 8388),
            ),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        val names = plan.upsert.map { it.name }
        assertThat(names).containsNoDuplicates()
        assertThat(names[1]).endsWith("2")
    }

    @Test
    fun `逐行错误数被带出`() {
        val plan = SubscriptionImport.plan(
            outcome(ssNode(), lineErrors = 3),
            existing = emptyList(),
            sourceTag = SubscriptionImport.sourceTag(subUrl),
            credentials = null,
        )

        assertThat(plan.lineErrorCount).isEqualTo(3)
        assertThat(plan.totalCount).isEqualTo(1)
    }

    // -----------------------------------------------------------------------

    private fun b64url(s: String): String =
        Base64.getUrlEncoder().withoutPadding().encodeToString(s.toByteArray(Charsets.UTF_8))
}
