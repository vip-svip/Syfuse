package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.SubscriptionInfo

/**
 * 订阅内容的统一入口：自动识别形态（分享链接列表 / base64 包裹 / Clash YAML），
 * 产出同一份 [ShareNode] 列表。
 *
 * ## 为什么需要自动识别
 *
 * 用户拿到一个订阅只会**粘贴**，不会先判断"这是 Clash 配置还是 URI 列表"。
 * 要求用户自己选格式，等于把实现者的分类工作推给用户 —— 而分类错了的报错
 * （"0 个节点"）看起来像订阅坏了，用户无从下手。
 *
 * ## 识别规则（按优先级，**先验后猜**）
 *
 * 1. 出现零缩进的 `proxies:` 行  → **Clash YAML**
 * 2. 整体是一个合法的 base64 串   → 解码后**重新识别**：含 `://` 当链接列表，
 *    含 `proxies:` 当 Clash 配置
 * 3. 否则                       → 按"每行一个 URI"解析
 *
 * ### 关于第 1 条与第 2 条的先后
 *
 * 顺序本身**不是**安全边界 —— 我用变异测试验证过：交换两条的判断顺序，
 * 现有全部用例的结论不变。真正把 Clash 配置挡在 base64 门口之外的是
 * [decodeBase64Quietly] 的两个硬条件：**整体无换行** + **每个字符都在
 * base64 字母表内**。一份 YAML 只要含一个空格或换行就解不动。
 *
 * 所以第 1 条放在前面是**防御性**的（少一次无谓的字母表扫描），
 * 不是"顺序错了就会解析失败"那样的强耦合。谁要重排顺序，请连带把
 * 这一并读一遍，别把它当成铁律。
 *
 * ## 部分成功语义
 *
 * 三种形态共用同一承诺：**坏行不毁掉整份订阅**。分享链接路径由
 * [ShareLink.parseSubscription] 保证，Clash 路径由 [ClashProxies] 保证
 * （无法映射的条目跳过而非报错）。
 */
object SubscriptionSource {

    /** 订阅解析的完整结果。 */
    data class Outcome(
        val nodes: List<ShareNode>,
        /** 识别出的形态，用于 UI 显示"这是什么类型的订阅"与排障。 */
        val format: Format,
        /** 逐行错误（分享链接路径）；Clash 路径跳过条目不计错误。 */
        val lineErrors: List<SubscriptionLineError> = emptyList(),
        /** 订阅头信息（`subscription-userinfo` 响应头），调用方有头时才填。 */
        val info: SubscriptionInfo = SubscriptionInfo(),
    ) {
        /** 用户可读的一句话结果。 */
        fun summary(): String {
            val f = when (format) {
                Format.SHARE_LINKS -> "分享链接"
                Format.BASE64_LINKS -> "Base64 分享链接"
                Format.CLASH_YAML -> "Clash 配置"
            }
            val bad = if (lineErrors.isEmpty()) "" else "，${lineErrors.size} 行无法解析"
            return "识别为$f，导入 ${nodes.size} 个节点$bad"
        }
    }

    enum class Format { SHARE_LINKS, BASE64_LINKS, CLASH_YAML }

    /**
     * 解析订阅内容。
     *
     * @param userInfoHeader 可选的 `subscription-userinfo` 响应头原文。
     * @return 成功时一定带 [Outcome.nodes]（可为空——空订阅是合法结果，不是错误）。
     */
    fun parse(content: String, userInfoHeader: String? = null): Outcome {
        val info = NodeMapping.parseSubscriptionUserInfo(userInfoHeader)

        if (looksLikeClash(content)) {
            val nodes = ClashProxies.parse(content)
            return Outcome(nodes = nodes, format = Format.CLASH_YAML, info = info)
        }

        val trimmed = content.trim()
        // 整体 base64：整份内容没有换行且不含 "://"，解码成功 → 是包裹过的内容。
        if (!trimmed.contains("://") && !trimmed.contains('\n') && trimmed.isNotEmpty()) {
            val decoded = decodeBase64Quietly(trimmed)
            if (decoded != null) {
                // 解码后**重新识别形态**，而不是假定"解出来一定是链接列表"。
                //
                // 少了这一步会漏一类真实订阅：**base64 包裹的 Clash 配置**。
                //   - 它整体是一份合法 base64（Hub 的"通用订阅"经常这么发）；
                //   - 但解码出来的文本里**没有 "://"**（Clash 用的是 YAML 键值）；
                //   - 于是旧逻辑直接回落到"每行一个 URI"路径，一行都解析不出来，
                //     用户看到"导入 0 个节点"，而订阅本身是好的。
                //
                // 这里只做**一层**递地识别，不再递归调用 parse：
                // base64 套 base64 不是真实存在的形态，而无限递归的代价是栈溢出。
                if (decoded.contains("://")) {
                    val r = ShareLink.parseSubscription(decoded)
                    return Outcome(
                        nodes = r.nodes,
                        format = Format.BASE64_LINKS,
                        lineErrors = r.errors,
                        info = info,
                    )
                }
                if (looksLikeClash(decoded)) {
                    return Outcome(nodes = ClashProxies.parse(decoded), format = Format.CLASH_YAML, info = info)
                }
            }
        }

        val r = ShareLink.parseSubscription(content)
        return Outcome(nodes = r.nodes, format = Format.SHARE_LINKS, lineErrors = r.errors, info = info)
    }

    /**
     * 是否像 Clash 配置。
     *
     * 判据：存在**零缩进**的 `proxies:` 行，且其值为**空**或**空流式序列** `[]`。
     * 不要求一定是文件第一行 —— 完整 Clash 配置里 `proxies:` 常在 `mixed-port` 之后。
     *
     * 为什么必须认 `proxies: []`：它是"服务端一个节点都没配"时的合法输出。
     *   旧判据只认空值，于是这类配置被判成"既不是 Clash 也不是 base64"，
     *   回落到"每行一个 URI"路径 —— 用户看到"导入 0 个节点"，
     *   而正确行为是**识别为 Clash 并如实报告 0 个节点**（格式对了，只是没内容）。
     */
    internal fun looksLikeClash(content: String): Boolean =
        content.split('\n').any { raw ->
            val line = ClashProxies.stripComment(raw).trimEnd()
            if (line.isBlank()) return@any false
            val trimmed = line.trimStart()
            val indent = line.length - trimmed.length
            if (indent != 0 || !trimmed.startsWith("proxies:")) return@any false
            val value = trimmed.substringAfter(':', "").trim()
            // 空值（块状）或空流式序列 `[]` / `[ ]` 都算。
            value.isEmpty() || value == "[]" || value == "[ ]"
        }

    /** base64 解码失败时返回 null 而不是抛异常 —— 识别阶段不该报错。 */
    internal fun decodeBase64Quietly(text: String): String? {
        val t = text.trim()
        // base64 字母表：A-Z a-z 0-9 + / =（URL-safe 变体用 - _）
        if (t.isEmpty()) return null
        if (!t.all { it.isLetterOrDigit() || it == '+' || it == '/' || it == '=' || it == '-' || it == '_' }) {
            return null
        }
        // 长度必须是 4 的倍数（去掉 padding 后也按 4 对齐判断）
        val body = t.trimEnd('=')
        if (body.isEmpty() || body.length % 4 == 1) return null
        return runCatching {
            val bytes = if (t.contains('-') || t.contains('_')) {
                java.util.Base64.getUrlDecoder().decode(t)
            } else {
                java.util.Base64.getDecoder().decode(t)
            }
            String(bytes, Charsets.UTF_8)
        }.getOrNull()
    }
}
