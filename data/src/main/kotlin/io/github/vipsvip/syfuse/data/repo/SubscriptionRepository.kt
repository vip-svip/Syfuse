package io.github.vipsvip.syfuse.data.repo

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.data.db.ProxyNodeDao
import io.github.vipsvip.syfuse.data.db.toDomain
import io.github.vipsvip.syfuse.data.db.toEntity
import io.github.vipsvip.syfuse.data.security.CredentialVault
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.domain.node.NodeMapping
import io.github.vipsvip.syfuse.domain.node.SubscriptionImport
import io.github.vipsvip.syfuse.domain.node.SubscriptionSource
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import javax.inject.Inject
import javax.inject.Singleton

/**
 * 订阅导入仓库：抓取 → 解析 → 落库，一条链路走完。
 *
 * ## 为什么必须把凭据交给 [CredentialVault]（R5）
 *
 * 分享链接里的口令/UUID 是明文。若直接随节点落库，一次 `adb backup`
 * 或数据库文件拷贝就全部带走了。所以明文**只在内存里经过一次**：
 * 立刻加密、只留引用。这也是 [NodeMapping.CredentialSink] 存在的意义 ——
 * 工程里能把明文写进加密容器的地方有且只有一个名字，审计时只看这一处。
 *
 * ## 抓取时的几个"实战细节"
 *
 * 这些不是洁癖，是机场侧真实存在的拦截行为，漏一个就表现为"订阅拉不下来"：
 *
 *  - **User-Agent**：相当多机场会挡掉 JDK 默认的 `Java/xx` UA。发送一个
 *    主流客户端的 UA 是通行做法（见 [DEFAULT_USER_AGENT]）。
 *  - **跟随重定向**：机场的订阅链接普遍是 302 到 CDN 的真实地址。
 *  - **响应体上限**：订阅是攻击面 —— 一份"订阅"可以是几百 MB 的垃圾，
 *    从而把 app 拖到 OOM。这里硬性截断（[MAX_BODY_BYTES]），宁可报错不硬吃。
 */
@Singleton
class SubscriptionRepository @Inject constructor(
    private val nodeDao: ProxyNodeDao,
    private val vault: CredentialVault,
) {

    /**
     * 抓取并导入一个订阅 URL。
     *
     * @return 导入结果；失败时返回带用户可读信息的 [SyfuseResult.Failure]。
     */
    suspend fun importFromUrl(url: String): SyfuseResult<SubscriptionImport.Plan> {
        val target = url.trim()
        if (target.isEmpty()) return failure("订阅地址为空", "请先填写订阅链接")
        val scheme = runCatching { URL(target).protocol.lowercase() }.getOrNull()
        if (scheme != "http" && scheme != "https") {
            return failure("订阅地址不是 http/https 链接：$target", "请检查订阅地址是否完整")
        }

        val fetched = withContext(Dispatchers.IO) { fetch(target) }
            ?: return failure(
                "无法访问订阅地址",
                "请检查网络与订阅地址；若地址需要代理，请先在系统代理/VPN 设置中放行本应用",
            )

        return importFromContent(
            content = fetched.body,
            source = target,
            userInfoHeader = fetched.userInfoHeader,
        )
    }

    /**
     * 导入一段**已有内容**（用户手动粘贴的订阅文本 / 配置）。
     *
     * @param source 来源标识（订阅 URL）。传 null 表示手动粘贴 ——
     *   不产出可清理的来源标签，因此重新粘贴不会删除旧节点。
     */
    suspend fun importFromContent(
        content: String,
        source: String? = null,
        userInfoHeader: String? = null,
    ): SyfuseResult<SubscriptionImport.Plan> {
        if (content.isBlank()) return failure("订阅内容为空", "服务端返回了空白内容，请稍后重试")

        val outcome = SubscriptionSource.parse(content, userInfoHeader)
        if (outcome.nodes.isEmpty() && outcome.lineErrors.isNotEmpty()) {
            return SyfuseResult.Failure(
                SyfuseError(
                    code = ErrorCode.SUBSCRIPTION_FORMAT_UNKNOWN,
                    technical = "0 nodes, ${outcome.lineErrors.size} line errors",
                    userMessage = "订阅内容无法解析（${outcome.lineErrors.size} 行全部失败）",
                    hint = "请确认这是代理订阅而不是普通网页；它可能是 Clash 配置或分享链接列表",
                ),
            )
        }

        val existing = withContext(Dispatchers.IO) {
            nodeDao.observeAll().first().map { it.toDomain() }
        }

        val plan = SubscriptionImport.plan(
            outcome = outcome,
            existing = existing,
            sourceTag = SubscriptionImport.sourceTag(source),
            credentials = vaultSink(),
        )

        withContext(Dispatchers.IO) {
            if (plan.upsert.isNotEmpty()) nodeDao.upsertAll(plan.upsert.map { it.toEntity() })
            plan.staleIds.forEach { nodeDao.delete(it) }
        }

        return SyfuseResult.Success(plan)
    }

    /** 当前全部节点（只读，供导入计划比对与 UI 展示）。 */
    suspend fun currentNodes(): List<ProxyNode> = withContext(Dispatchers.IO) {
        nodeDao.observeAll().first().map { it.toDomain() }
    }

    // ------------------------------------------------------------------ 内部

    /**
     * 把凭据保险箱包成 [NodeMapping.CredentialSink]。
     *
     * alias 用**单一固定值**而不是每节点一个：AndroidKeyStore 里每条密钥都有
     * 真实开销（TEE 存储与首次生成耗时），几百个节点就是几百条密钥，
     * 而它们保护的是同一批数据。一把密钥 + 每条记录各自随机 IV 的安全性等价
     * （GCM 的 IV 由 `Cipher.init` 每次随机生成，见 [CredentialVault]）。
     */
    private fun vaultSink(): NodeMapping.CredentialSink = NodeMapping.CredentialSink { secret ->
        vault.encrypt(secret, CREDENTIAL_ALIAS)
    }

    private data class Fetched(val body: String, val userInfoHeader: String?)

    /** 抓取订阅。任何网络异常都收敛为 null，由调用方转成用户可读失败。 */
    private fun fetch(url: String): Fetched? = runCatching {
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            instanceFollowRedirects = true
            connectTimeout = CONNECT_TIMEOUT_MS
            readTimeout = READ_TIMEOUT_MS
            // 机场侧普遍按 UA 放行/拦截，这是"订阅拉不下来"最常见的原因之一。
            setRequestProperty("User-Agent", DEFAULT_USER_AGENT)
            setRequestProperty("Accept", "*/*")
        }
        try {
            val code = connection.responseCode
            if (code !in 200..299) return null
            val body = if (code == 204) {
                ""
            } else {
                connection.inputStream.bufferedReader(Charsets.UTF_8).use { reader ->
                    readCapped(reader)
                }
            }
            Fetched(body = body, userInfoHeader = connection.getHeaderField(USER_INFO_HEADER))
        } finally {
            connection.disconnect()
        }
    }.getOrNull()

    /**
     * 限量读取，超出 [MAX_BODY_BYTES] 立刻抛 [IOException]。
     *
     * 用"读够上限就抛"而不是"读完后判断长度"：后者会把整份超大响应
     * 完整读进内存才拒绝，OOM 已经发生了。这是防御恶意/异常服务端的实际措施。
     */
    private fun readCapped(reader: java.io.Reader): String {
        val buffer = StringBuilder()
        val chunk = CharArray(8 * 1024)
        while (true) {
            val read = reader.read(chunk)
            if (read < 0) break
            buffer.append(chunk, 0, read)
            if (buffer.length > MAX_BODY_BYTES) {
                throw IOException("subscription body exceeds $MAX_BODY_BYTES chars")
            }
        }
        return buffer.toString()
    }

    private fun failure(technical: String, userMessage: String, hint: String? = null) =
        SyfuseResult.Failure(
            SyfuseError(
                code = ErrorCode.SUBSCRIPTION_FETCH_FAILED,
                technical = technical,
                userMessage = userMessage,
                hint = hint,
            ),
        )

    private companion object {
        const val CONNECT_TIMEOUT_MS = 15_000
        const val READ_TIMEOUT_MS = 20_000
        const val MAX_BODY_BYTES = 8 * 1024 * 1024

        /** `subscription-userinfo` 响应头：携带流量与到期信息。 */
        const val USER_INFO_HEADER = "subscription-userinfo"

        /**
         * 抓取订阅时使用的 User-Agent。
         *
         * 选择理由：**兼容性优先**。这不是伪装成某个具体产品去骗服务端，
         * 而是绕开"只按 UA 白名单放行"的机场策略 —— 这类策略会连带动
         * 拦截 JDK 默认 UA（`Java/17.0.x`），那是纯技术副产物而非本应用的身份。
         * 如需调整，改这里一处即可，抓取逻辑不散落 UA。
         */
        const val DEFAULT_USER_AGENT = "clash-verge/2.0.0"

        /** 节点凭据的统一 Keystore 别名（理由见 [vaultSink] 的注释）。 */
        const val CREDENTIAL_ALIAS = "syfuse.node.credentials"
    }
}
