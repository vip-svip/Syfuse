package io.github.vipsvip.syfuse.core.rule

/**
 * 域名的反向标签 Trie（suffix trie）。
 *
 * 用途：把 `||example.com^` 一类域名锚定规则编译进 Trie，使 DNS 层的单次查询
 * 从"线性扫描 N 条规则"降为"按标签深度 O(标签数)"。这是 5.4 性能目标
 * （百万级规则、单次匹配 p99 < 1ms）的基础结构。
 *
 * 反向存储（`com.example.ads` → `ads` → `example` → `com`）使后缀匹配天然成立：
 * 查 `a.b.example.com` 时会依次经过 `com`、`example`、`b`、`a`，路径上挂的规则全部命中。
 */
class DomainTrie<T> {
    private class Node<T> {
        val children: HashMap<String, Node<T>> = HashMap(2)
        var values: MutableList<T>? = null
    }

    private val root = Node<T>()
    private var valueCount = 0

    /** 挂载一条规则到指定域名。 */
    fun put(domain: String, value: T) {
        val labels = labelsOf(domain)
        if (labels.isEmpty()) return
        var node = root
        for (label in labels) {
            node = node.children.getOrPut(label) { Node() }
        }
        val list = node.values ?: ArrayList<T>(2).also { node.values = it }
        list.add(value)
        valueCount++
    }

    /**
     * 查出所有后缀匹配的 value。返回顺序为**最长匹配优先**：
     * 更具体的规则（`ads.example.com`）排在更宽泛的规则（`example.com`）之前。
     * 引擎最终仍按优先级模型裁决，此顺序只影响链路展示的可读性。
     */
    fun lookup(host: String): List<T> {
        val labels = labelsOf(host)
        if (labels.isEmpty()) return emptyList()
        var node = root
        val buckets = ArrayList<List<T>>(labels.size + 1)
        node.values?.let { buckets.add(it) }
        for (label in labels) {
            node = node.children[label] ?: break
            node.values?.let { buckets.add(it) }
        }
        if (buckets.isEmpty()) return emptyList()
        val out = ArrayList<T>(buckets.sumOf { it.size })
        for (i in buckets.indices.reversed()) out.addAll(buckets[i])
        return out
    }

    val size: Int get() = valueCount

    private fun labelsOf(domain: String): List<String> =
        domain.lowercase().trim().trimEnd('.').split('.').filter { it.isNotEmpty() }.asReversed()

    companion object {
        /**
         * host 是否等于 domain 或为其子域。
         * 注意这是**标签级**匹配：`notexample.com` 不匹配 `example.com`，
         * 避免用字符串 endsWith 造成的越界放行/拦截。
         */
        fun hostMatchesDomain(host: String, domain: String): Boolean {
            val h = host.lowercase().trimEnd('.')
            val d = domain.lowercase().trimEnd('.').trimStart('.')
            if (d.isEmpty()) return false
            if (h == d) return true
            return h.length > d.length && h.endsWith(d) && h[h.length - d.length - 1] == '.'
        }
    }
}
