package io.github.vipsvip.syfuse.core.rule

/**
 * IPv4 CIDR 最长前缀匹配 Trie（即 6.1 工程要求中的 "IP Patricia Trie" 的等价实现）。
 *
 * 用途：IP 网段类规则（网络层封禁 6.4、IP-CIDR 分流 5.4）需要在 O(32) 内完成匹配。
 * 用二叉 Trie 而非区间排序表，是为了支持"最长前缀优先"这一语义——
 * 排序表只能回答"是否命中"，无法回答"命中的是哪一条"。
 *
 * IPv6 未在此实现：M1 只交付 IPv4 路径，IPv6 记录在未决清单中（见 README 未决与风险）。
 */
class Ipv4CidrTrie<T> {
    private class Node<T> {
        var zero: Node<T>? = null
        var one: Node<T>? = null
        var value: T? = null
    }

    private val root = Node<T>()
    private var entryCount = 0

    /** 插入一条 CIDR。prefix 省略时按 /32 处理。非法输入返回 false，由调用方报错到行号。 */
    fun put(cidr: String, value: T): Boolean {
        val slash = cidr.indexOf('/')
        val ipPart = if (slash >= 0) cidr.substring(0, slash) else cidr
        val prefix = if (slash >= 0) cidr.substring(slash + 1).toIntOrNull() ?: return false else 32
        if (prefix !in 0..32) return false
        val ip = parseIpv4(ipPart) ?: return false

        var node = root
        for (bit in 31 downTo (32 - prefix)) {
            val set = (ip ushr bit) and 1 == 1
            node = if (set) {
                node.one ?: Node<T>().also { node.one = it }
            } else {
                node.zero ?: Node<T>().also { node.zero = it }
            }
        }
        node.value = value
        entryCount++
        return true
    }

    /** 最长前缀匹配。未命中返回 null。 */
    fun lookup(ip: String): T? {
        val addr = parseIpv4(ip) ?: return null
        var node = root
        var best = root.value
        for (bit in 31 downTo 0) {
            val set = (addr ushr bit) and 1 == 1
            node = (if (set) node.one else node.zero) ?: break
            node.value?.let { best = it }
        }
        return best
    }

    val size: Int get() = entryCount

    companion object {
        /** 严格解析点分十进制 IPv4；拒绝前导零以外的任何异常写法，避免把 "1.2.3" 当成合法地址。 */
        fun parseIpv4(text: String): Int? {
            val parts = text.trim().split('.')
            if (parts.size != 4) return null
            var acc = 0
            for (part in parts) {
                if (part.isEmpty() || part.length > 3) return null
                if (!part.all { it in '0'..'9' }) return null
                val v = part.toInt()
                if (v !in 0..255) return null
                acc = (acc shl 8) or v
            }
            return acc
        }
    }
}
