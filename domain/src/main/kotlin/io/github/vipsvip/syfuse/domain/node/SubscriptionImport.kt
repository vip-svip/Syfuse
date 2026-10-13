package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.ProxyNode
import java.security.MessageDigest

/**
 * 订阅导入的**纯逻辑**部分：把 [SubscriptionSource.Outcome] 变成一份可执行的落库计划。
 *
 * ## 为什么要单独拆出来
 *
 * 网络抓取（`HttpURLConnection`）与数据库（Room）都带不进 JVM 单测，
 * 而"导入会不会重复、会不会丢用户收藏、下线节点能不能清掉"这些**判断**恰恰是最该被测的。
 * 所以这里只做判断，`data` 层的仓库只做 IO —— 两边都能各自测干净。
 *
 * ## 三条关键决策，以及它们的理由
 *
 * **① 节点 id = SHA-256(节点 key)，不是 key 本身。**
 *    [ShareNode.key] 里**含明文口令/UUID**（"同服务器同端口但密码不同是两个不同节点"）。
 *    若直接拿它当主键，明文凭据就落进了 Room 的普通文本列 —— R5 作废。
 *    哈希是单向的：去重照样稳定，凭据不外泄。
 *
 * **② 重新导入同一订阅时，保留用户的收藏与标签。**
 *    [NodeMapping.toProxyNode] 永远产出 `favorite=false`、`tags=empty`（它是"从零新建"
 *    的语义）。若直接拿它覆盖，用户辛苦收藏的节点会在下一次订阅刷新后悄悄取消收藏 ——
 *    这种**静默丢失用户状态**的 bug 在真实产品里极难被发觉。所以这里按 id 合并。
 *
 * **③ 下线节点的清理只清理"仅属于本订阅"的节点。**
 *    同一个节点可能同时出现在两份订阅里。若按"本订阅已不含它"就删，
 *    会把另一份订阅还在用的节点删掉。判据是：该节点的 `sub:` 标签**只有本订阅一个**
 *    （即它是被本订阅独占引进来的）。用标签而不是新加数据库列，
 *    是为了不改 schema（`ProxyNodeEntity` 无需迁移）。
 */
object SubscriptionImport {

    /** 订阅来源标签前缀。`sub:` + URL 的短哈希，例如 `sub:9f2c1ab4`。 */
    const val SOURCE_TAG_PREFIX = "sub:"

    /** 一次导入的落库计划。 */
    data class Plan(
        /** 需要 upsert 的节点（已合并来源标签、保留既有收藏）。 */
        val upsert: List<ProxyNode>,
        /** 仅属于本订阅、但本次内容里已不存在的节点 id —— 调用方应删除。 */
        val staleIds: List<String>,
        val newCount: Int,
        val updatedCount: Int,
        val removedCount: Int,
        val format: SubscriptionSource.Format,
        /** 逐行解析失败数（分享链接路径）。 */
        val lineErrorCount: Int,
    ) {
        val totalCount: Int get() = upsert.size
    }

    /**
     * 由来源（订阅 URL 或"手动粘贴"）算出稳定的来源标签。
     *
     * @param source 订阅 URL；null 表示手动粘贴的内容（不产出可清理的标签）。
     */
    fun sourceTag(source: String?): String? {
        if (source.isNullOrBlank()) return null
        val digest = sha256Hex(source.trim())
        return SOURCE_TAG_PREFIX + digest.take(8)
    }

    /**
     * 制定导入计划。
     *
     * @param outcome [SubscriptionSource.parse] 的产物。
     * @param existing 数据库中现存的全部节点（用于判断新增/更新与独占下线）。
     * @param sourceTag [sourceTag] 的返回值；手动粘贴传 null。
     * @param credentials 凭据落库回调；传 null 时 `credentialRef` 为空
     *   （合法中间态：节点存在但尚未登记凭据，连接时会报"缺凭据"而不是崩）。
     */
    fun plan(
        outcome: SubscriptionSource.Outcome,
        existing: List<ProxyNode>,
        sourceTag: String?,
        credentials: NodeMapping.CredentialSink?,
    ): Plan {
        val existingById = existing.associateBy { it.id }
        val usedNames = mutableMapOf<String, Int>()

        val upsert = outcome.nodes.mapIndexedNotNull { index, share ->
            // port <= 0 的行是坏行：保留它会在 UI 里出现一个连不上的幽灵节点。
            // ShareLink 的解析失败已经计入 lineErrors，这里直接跳过。
            if (share.port <= 0 || share.server.isBlank()) return@mapIndexedNotNull null

            val node = NodeMapping.toProxyNode(
                node = share,
                id = nodeIdOf(share),
                credentials = credentials,
                sortIndex = index,
            )

            // 名字去重：同一份订阅里出现重名时追加序号，避免 UI 上一片同名节点
            // 分不清谁是谁。（机场重名很常见。）
            val seen = usedNames.getOrDefault(node.name, 0)
            usedNames[node.name] = seen + 1
            val uniqueName = if (seen == 0) node.name else "${node.name} ${seen + 1}"

            val previous = existingById[node.id]
            val mergedTags = mergeTags(previous?.tags, sourceTag)
            node.copy(
                name = uniqueName,
                tags = mergedTags,
                // 保留用户收藏：重新导入订阅不该取消用户的收藏（决策②）。
                favorite = previous?.favorite ?: node.favorite,
                // 已有延迟数据就留着：刷新订阅不是重新测速的理由。
                latencyMs = previous?.latencyMs ?: node.latencyMs,
            )
        }

        val upsertIds = upsert.map { it.id }.toSet()

        // 下线节点清理：只清理"标签里除本订阅外没有任何其他 sub: 来源"的节点（决策③）。
        //
        // ⚠️ 这里必须用 startsWith 而不是 `in`：`in` 在 List 上是**相等**判断，
        //    而标签形如 `sub:9f2c1ab4`，永远不等于字面量 `"sub:"`。
        //    这个 bug 被测试当场抓到（staleIds 恒为空 = 下线节点永远清不掉）。
        val staleIds = if (sourceTag == null) {
            emptyList()
        } else {
            existing.mapNotNull { node ->
                if (node.id in upsertIds) return@mapNotNull null
                val hasAnySourceTag = node.tags.any { it.startsWith(SOURCE_TAG_PREFIX) }
                if (!hasAnySourceTag) return@mapNotNull null
                val otherSources = node.tags.filter {
                    it.startsWith(SOURCE_TAG_PREFIX) && it != sourceTag
                }
                if (otherSources.isNotEmpty()) return@mapNotNull null
                node.id
            }
        }

        return Plan(
            upsert = upsert,
            staleIds = staleIds,
            newCount = upsert.count { it.id !in existingById },
            updatedCount = upsert.count { it.id in existingById },
            removedCount = staleIds.size,
            format = outcome.format,
            lineErrorCount = outcome.lineErrors.size,
        )
    }

    /**
     * 节点的稳定主键：SHA-256([ShareNode.key]) 的十六进制。
     *
     * 取**完整** 64 位十六进制而不是截断：截断会升高碰撞概率，而碰撞的代价是
     * 两个不同节点互相覆盖（静默丢节点）。全长的存储成本差异可以忽略。
     */
    fun nodeIdOf(share: ShareNode): String = sha256Hex(share.key())

    /** 合并来源标签：保留用户手工加的标签与本订阅标签，去掉重复。 */
    internal fun mergeTags(existing: List<String>?, sourceTag: String?): List<String> {
        if (sourceTag == null) return existing ?: emptyList()
        val merged = LinkedHashSet<String>()
        existing?.forEach { merged.add(it) }
        merged.add(sourceTag)
        return merged.toList()
    }

    private fun sha256Hex(text: String): String =
        MessageDigest.getInstance("SHA-256")
            .digest(text.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }
}
