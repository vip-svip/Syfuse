package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.ProfileType
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import io.github.vipsvip.syfuse.domain.model.SubscriptionInfo

/**
 * [ShareNode]（分享链接解析出来的一次性事实）与 [ProxyNode]（可持久化的领域模型）
 * 之间的映射。
 *
 * ## 为什么要分成两个类型
 *
 * [ShareNode] 只承载"这一条链接说了什么"，字段与 Go 侧 `Node` 一一对应，便于跨语言对拍；
 * [ProxyNode] 是**库里的**节点，带 `id` / `sortIndex` / `favorite` / 加密凭据引用（R5）。
 * 把两者混成一个类型，会迫使解析函数去关心"主键怎么生成"这类与本职责无关的事，
 * 也会让跨语言契约被持久化细节污染。
 *
 * ## 凭据去向（R5 硬要求）
 *
 * [ProxyNode.credentialRef] 是**加密容器的引用**，不是明文。
 * 本映射**不**把明文凭据写进 [ProxyNode] —— 它只产出一个引用名，
 * 明文由调用方交给凭据库（`data` 层的加密容器）保管。
 * 若调用方传了 [CredentialSink]，则由它负责落库；不传时引用为空，
 * 表示"这个节点还没有被登记凭据"，属于合法中间态。
 */
object NodeMapping {

    /**
     * 凭据落库的回调。
     *
     * 刻意做成接口而不是"直接返回明文"：让**唯一**能把明文写进加密容器的地方
     * 有明确的名字，审计时只需要看这一个实现。
     * 签名接收节点 key（稳定标识）与明文凭据，返回 `credentialRef`。
     */
    fun interface CredentialSink {
        fun store(nodeKey: String, secret: String): String
    }

    /** [ShareNode] → [ProxyNode]。 */
    fun toProxyNode(
        node: ShareNode,
        id: String,
        credentials: CredentialSink? = null,
        sortIndex: Int = 0,
    ): ProxyNode {
        val secret = if (node.uuid.isNotEmpty()) node.uuid else node.password
        val ref = if (secret.isNotEmpty() && credentials != null) {
            credentials.store(node.key(), secret)
        } else {
            null
        }
        // 字段装配全部委托给 ShareNodeMapping —— 那里是唯一的实现点。
        // 早期这里有一份"自己的"装配逻辑，与 ShareNodeMapping 形成了两个真相来源，
        // 且比它少了三类字段（见下方 toProxyNode 的注释）。合并成一条路径后，
        // "导入的节点长什么样"只有一个地方需要维护。
        return node.toProxyNode(
            id = id,
            sortIndex = sortIndex,
            credentialRef = ref,
        )
    }

    /** [ProxyProtocol] ← 协议原文。未知协议回落到 [ProxyProtocol.SOCKS5] 之外的空缺时抛错。 */
    fun protocolOf(raw: String): ProxyProtocol = when (raw.lowercase()) {
        "ss" -> ProxyProtocol.SS
        "ssr" -> ProxyProtocol.SSR
        "vmess" -> ProxyProtocol.VMESS
        "vless" -> ProxyProtocol.VLESS
        "trojan" -> ProxyProtocol.TROJAN
        "hysteria2", "hy2" -> ProxyProtocol.HYSTERIA2
        "http", "https" -> ProxyProtocol.HTTP
        "socks", "socks5" -> ProxyProtocol.SOCKS5
        else -> throw ShareLinkException("未知协议无法映射到领域模型: $raw")
    }

    /**
     * 订阅头里的流量 / 到期信息解析。
     *
     * 机场普遍把信息塞在响应头 `subscription-userinfo`，格式形如：
     * ```
     * upload=1234; download=5678; total=100000; expire=1735689600
     * ```
     * 字段可能缺失、顺序不固定、值可能是非数字垃圾 —— 一律按"能取到就取"处理，
     * 取不到就留 null（对应 `SubscriptionInfo` 各字段可空的设计）。
     *
     * @param headerValue 响应头原文；null 或空返回仅含 [SubscriptionInfo.rawTitle] 的结果。
     */
    fun parseSubscriptionUserInfo(headerValue: String?): SubscriptionInfo {
        if (headerValue.isNullOrBlank()) return SubscriptionInfo()

        var upload: Long? = null
        var download: Long? = null
        var total: Long? = null
        var expire: Long? = null

        for (part in headerValue.split(';', ',')) {
            val t = part.trim()
            if (t.isEmpty()) continue
            val eq = t.indexOf('=')
            if (eq <= 0) continue
            val key = t.substring(0, eq).trim().lowercase()
            val rawValue = t.substring(eq + 1).trim()
            val value = rawValue.toLongOrNull() ?: continue
            when (key) {
                "upload" -> upload = value
                "download" -> download = value
                "total" -> total = value
                "expire" -> expire = value
            }
        }

        // expire 为 0 或负数表示"永不过期"，按没有到期信息处理，避免 UI 显示 1970 年。
        if (expire != null && expire <= 0) expire = null

        return SubscriptionInfo(
            uploadBytes = upload,
            downloadBytes = download,
            totalBytes = total,
            expireAtEpochSeconds = expire,
            rawTitle = headerValue.trim(),
        )
    }
}

/**
 * 订阅更新时的差异。
 *
 * 三类：新增 / 删除 / 改动。**改动**的判定不是"名字变了"——
 * 名字变了但连接参数没变属于"同一个节点改了名"，归入 [renamed] 而不是 [changed]，
 * 否则每次机场调整节点命名都会在界面上刷出一大片"改动"。
 */
data class SubscriptionDiff(
    val added: List<ShareNode> = emptyList(),
    val removed: List<ShareNode> = emptyList(),
    /** 连接参数（凭据 / 传输层）发生变化的节点，携带新旧两版。 */
    val changed: List<ChangedNode> = emptyList(),
    /** 仅名称变化、连接参数不变的节点。 */
    val renamed: List<RenamedNode> = emptyList(),
    /** 新旧完全相同的节点数（用于 UI 显示"无变化"）。 */
    val unchangedCount: Int = 0,
) {
    val hasChanges: Boolean
        get() = added.isNotEmpty() || removed.isNotEmpty() || changed.isNotEmpty() || renamed.isNotEmpty()

    /** 一句话摘要，直接可用于 UI toast / 通知。 */
    fun summary(): String {
        if (!hasChanges) return "订阅无变化（$unchangedCount 个节点）"
        val parts = buildList {
            if (added.isNotEmpty()) add("新增 ${added.size}")
            if (removed.isNotEmpty()) add("删除 ${removed.size}")
            if (changed.isNotEmpty()) add("改动 ${changed.size}")
            if (renamed.isNotEmpty()) add("改名 ${renamed.size}")
        }
        return "订阅已更新：" + parts.joinToString(" / ") + "（无变化 $unchangedCount）"
    }

    data class ChangedNode(val old: ShareNode, val new: ShareNode)
    data class RenamedNode(val oldName: String, val newName: String, val node: ShareNode)
}

/**
 * 订阅 diff：比较新旧两份节点列表。
 *
 * ## 匹配依据是 [ShareNode.key]（连接参数），不是名称
 *
 * 这样"机场把节点从『香港 01』改名为『HK-01』"不会产生 add+remove 的噪音，
 * 而是被识别成 [SubscriptionDiff.renamed]。反过来，若名称相同但凭据变了，
 * 会被识别成 [SubscriptionDiff.changed] —— 这是真实需要提醒用户的情形
 * （凭据轮换后旧连接会失败）。
 *
 * ## 为什么 key 变了却可能是"同一个节点"
 *
 * 若机场轮换了密码，key 会变，于是表现为 add+remove。这在信息论上无法与
 * "真的换了一个节点"区分开 —— 除非用名称做弱匹配。本实现**额外**做一层：
 * 先按 key 精确匹配；剩余的再按 (protocol, server, port) 三元组做弱匹配，
 * 命中则判为 changed（凭据轮换）而非 add+remove。这层弱匹配显著降低误报，
 * 且不会把"同主机同端口的两个不同节点"搞混 —— 因为弱匹配只在双方都**唯一**时生效。
 */
object SubscriptionDiffer {

    fun diff(old: List<ShareNode>, new: List<ShareNode>): SubscriptionDiff {
        val oldByKey = old.associateBy { it.key() }
        val newByKey = new.associateBy { it.key() }

        val added = mutableListOf<ShareNode>()
        val removed = mutableListOf<ShareNode>()
        val changed = mutableListOf<SubscriptionDiff.ChangedNode>()
        val renamed = mutableListOf<SubscriptionDiff.RenamedNode>()
        var unchanged = 0

        for (n in new) {
            val o = oldByKey[n.key()]
            when {
                o == null -> added += n
                o.name != n.name -> {
                    renamed += SubscriptionDiff.RenamedNode(o.name, n.name, n)
                    unchanged++
                }
                else -> unchanged++
            }
        }
        for (o in old) {
            if (newByKey[o.key()] == null) removed += o
        }

        // 弱匹配：把"凭据轮换"从 add+remove 里救回来。
        // 只在**唯一的** (protocol, server, port) 前缀上匹配，避免歧义误判。
        val pendingAdd = added.toMutableList()
        val pendingRemove = removed.toMutableList()
        val oldPrefixGroups = pendingRemove.groupBy { prefix(it) }
        val newPrefixGroups = pendingAdd.groupBy { prefix(it) }

        val resolvedAdds = mutableSetOf<ShareNode>()
        val resolvedRemoves = mutableSetOf<ShareNode>()
        for ((pfx, newGroup) in newPrefixGroups) {
            val oldGroup = oldPrefixGroups[pfx] ?: continue
            if (newGroup.size != 1 || oldGroup.size != 1) continue
            val n = newGroup.single()
            val o = oldGroup.single()
            resolvedAdds += n
            resolvedRemoves += o
            changed += SubscriptionDiff.ChangedNode(o, n)
        }
        pendingAdd.removeAll(resolvedAdds)
        pendingRemove.removeAll(resolvedRemoves)

        return SubscriptionDiff(
            added = pendingAdd,
            removed = pendingRemove,
            changed = changed,
            renamed = renamed,
            unchangedCount = unchanged,
        )
    }

    private fun prefix(n: ShareNode): String =
        "${n.protocolRaw.lowercase()}|${n.server.lowercase()}|${n.port}"
}
