package io.github.vipsvip.syfuse.domain.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * 领域模型。字段与第 10 部分的 schema 一一对应。
 *
 * 兼容性约定（第 10 部分要求）：
 *  - 所有可持久化的聚合根都带 [SCHEMA_VERSION]；
 *  - 反序列化时**未知字段保留不丢弃**（由 Json 配置的 ignoreUnknownKeys=false + 显式
 *    未知字段暂存实现；本工程选择在 JSON 层保留原始文本，见 ConfigBundle.rawExtras）；
 *  - 导入旧版本先备份原文件再迁移。
 */
const val SCHEMA_VERSION: Int = 1

/** 全局路由模式。命名必须与 Shadowrocket 一致：配置 / 代理 / 直连。 */
@Serializable
enum class RoutingMode {
    @SerialName("rule")
    RULE,

    @SerialName("global")
    GLOBAL,

    @SerialName("direct")
    DIRECT,
    ;

    /** UI 显示名，保持与主干概念同名（第 4 部分的去重裁决要求）。 */
    val displayName: String
        get() = when (this) {
            RULE -> "配置"
            GLOBAL -> "代理"
            DIRECT -> "直连"
        }
}

/** 配置档案来源。 */
@Serializable
enum class ProfileType {
    @SerialName("remote")
    REMOTE,

    @SerialName("local")
    LOCAL,
}

/** 配置档案。一个档案拥有独立的规则、DNS 与脚本。 */
@Serializable
data class Profile(
    val id: String,
    val name: String,
    val type: ProfileType = ProfileType.LOCAL,
    val sourceUrl: String? = null,
    /** 订阅自动更新间隔（小时）。0 表示不自动更新。 */
    val updateIntervalHours: Int = 0,
    val subscriptionInfo: SubscriptionInfo? = null,
    val rawConfig: String = "",
    val routingMode: RoutingMode = RoutingMode.RULE,
    val ruleSetIds: List<String> = emptyList(),
    val dnsConfig: DnsConfig = DnsConfig(),
    val scriptIds: List<String> = emptyList(),
    val createdAt: Long = 0L,
    val updatedAt: Long = 0L,
    val schemaVersion: Int = SCHEMA_VERSION,
)

/** 订阅信息（流量与到期）。字段可为空——并非所有机场都在订阅头里提供。 */
@Serializable
data class SubscriptionInfo(
    val uploadBytes: Long? = null,
    val downloadBytes: Long? = null,
    val totalBytes: Long? = null,
    val expireAtEpochSeconds: Long? = null,
    /** 订阅头里出现的自定义标题，例如 "剩余流量：12.3 GB"。原样保留便于展示。 */
    val rawTitle: String? = null,
)

/** 节点。 */
@Serializable
data class ProxyNode(
    val id: String,
    val name: String,
    val protocol: ProxyProtocol,
    val server: String,
    val port: Int,
    /** 口令 / UUID / 私钥等敏感字段。R5 要求加密存储，明文只存在于内存与加密容器中。 */
    val credentialRef: String? = null,
    val tls: TlsOptions? = null,
    val transport: TransportOptions? = null,
    val plugin: PluginOptions? = null,
    val tags: List<String> = emptyList(),
    val latencyMs: Int? = null,
    val favorite: Boolean = false,
    val sortIndex: Int = 0,
    /** 原始节点 JSON，用于 round-trip 不丢字段。 */
    val rawJson: String = "",
)

/** 协议。与 5.2 的覆盖清单一致。 */
@Serializable
enum class ProxyProtocol {
    @SerialName("ss") SS,
    @SerialName("ssr") SSR,
    @SerialName("vmess") VMESS,
    @SerialName("vless") VLESS,
    @SerialName("trojan") TROJAN,
    @SerialName("trojan-go") TROJAN_GO,
    @SerialName("hysteria") HYSTERIA,
    @SerialName("hysteria2") HYSTERIA2,
    @SerialName("tuic") TUIC,
    @SerialName("wireguard") WIREGUARD,
    @SerialName("snell") SNELL,
    @SerialName("http") HTTP,
    @SerialName("https") HTTPS,
    @SerialName("socks5") SOCKS5,
    @SerialName("ssh") SSH,
    @SerialName("anytls") ANYTLS,
    @SerialName("shadowtls") SHADOWTLS,
    @SerialName("juicity") JUICITY,
    @SerialName("mieru") MIERU,
}

/** TLS 参数（5.2：全开放）。 */
@Serializable
data class TlsOptions(
    val sni: String? = null,
    val alpn: List<String> = emptyList(),
    /** uTLS 指纹：chrome / firefox / safari / ios / random / 自定义。 */
    val fingerprint: String? = null,
    val skipCertVerify: Boolean = false,
    val allowInsecure: Boolean = false,
    val realityPublicKey: String? = null,
    val realityShortId: String? = null,
    /** 证书固定：期望的证书 SHA-256 指纹列表。 */
    val pinnedSha256: List<String> = emptyList(),
)

/** 传输层参数（5.2：TCP / WS / HTTP-Upgrade / gRPC / HTTP2 / QUIC / mKCP / HTTP-Obfs）。 */
@Serializable
data class TransportOptions(
    val type: TransportType = TransportType.TCP,
    val host: String? = null,
    val path: String? = null,
    val serviceName: String? = null,
    val headers: Map<String, String> = emptyMap(),
    val earlyData: Boolean = false,
    val maxEarlyData: Int? = null,
)

@Serializable
enum class TransportType {
    @SerialName("tcp") TCP,
    @SerialName("ws") WS,
    @SerialName("httpupgrade") HTTP_UPGRADE,
    @SerialName("grpc") GRPC,
    @SerialName("http2") HTTP2,
    @SerialName("quic") QUIC,
    @SerialName("mkcp") MKCP,
    @SerialName("http-obfs") HTTP_OBFS,
}

/** 插件（obfs / v2ray-plugin）。 */
@Serializable
data class PluginOptions(
    val name: String,
    val opts: Map<String, String> = emptyMap(),
)

/** 策略组。 */
@Serializable
data class ProxyGroup(
    val id: String,
    val name: String,
    val type: ProxyGroupType,
    val members: List<String> = emptyList(),
    val url: String? = null,
    val testIntervalSeconds: Int? = null,
    val toleranceMs: Int? = null,
    val timeoutMs: Int? = null,
    /** 链式代理（relay）的下一跳。 */
    val dialerProxy: String? = null,
    val nested: Boolean = false,
)

/** 策略组类型，与 5.2 一致。 */
@Serializable
enum class ProxyGroupType {
    @SerialName("select") SELECT,
    @SerialName("url-test") URL_TEST,
    @SerialName("fallback") FALLBACK,
    @SerialName("load-balance") LOAD_BALANCE,
    @SerialName("relay") RELAY,
}

/** 规则集。 */
@Serializable
data class RuleSet(
    val id: String,
    val name: String,
    val sourceUrl: String? = null,
    val localPath: String? = null,
    val format: RuleSetFormat = RuleSetFormat.DOMAIN,
    val updateIntervalHours: Int = 0,
    val ruleCount: Int = 0,
    val lastUpdated: Long = 0L,
    val sha256: String? = null,
    val enabled: Boolean = true,
)

/** 规则集格式，与 6.1 的四种格式一致。 */
@Serializable
enum class RuleSetFormat {
    @SerialName("domain") DOMAIN,
    @SerialName("ipcidr") IPCIDR,
    @SerialName("classical") CLASSICAL,
    @SerialName("mrs") MRS,
}

/** 规则条目。 */
@Serializable
data class RuleEntry(
    val id: String,
    val sourceId: String,
    val rawText: String,
    val kind: RuleKind,
    val action: RuleAction,
    val lineNo: Int = 0,
    val hitCount: Long = 0L,
    val order: Int = 0,
    /** 修饰符原文，保留以便 UI 展示与 round-trip。 */
    val modifierText: String? = null,
)

/** 规则语义类别，与 core:rule 的 RuleKind 保持同构。 */
@Serializable
enum class RuleKind {
    @SerialName("filter") FILTER,
    @SerialName("route") ROUTE,
    @SerialName("cosmetic") COSMETIC,
    @SerialName("hosts") HOSTS,
    @SerialName("block") BLOCK,
}

@Serializable
enum class RuleAction {
    @SerialName("block") BLOCK,
    @SerialName("allow") ALLOW,
    @SerialName("rewrite") REWRITE,
    @SerialName("proxy") PROXY,
    @SerialName("direct") DIRECT,
}

/** DNS 配置（5.5）。 */
@Serializable
data class DnsConfig(
    val servers: List<DnsServer> = emptyList(),
    val fallback: List<DnsServer> = emptyList(),
    val fallbackFilter: FallbackFilter = FallbackFilter(),
    val mode: DnsMode = DnsMode.NORMAL,
    val fakeIpPool: String = "198.18.0.0/15",
    val fakeIpTtlSeconds: Int = 1,
    val hosts: Map<String, String> = emptyMap(),
    val cacheSize: Int = 4096,
    val strategy: String = "prefer_ipv4",
    val respectRules: Boolean = false,
    val sniff: Boolean = true,
)

/** DNS 上游。每种协议独立配置。 */
@Serializable
data class DnsServer(
    val address: String,
    val protocol: DnsProtocol = DnsProtocol.UDP,
    val port: Int? = null,
    val path: String? = null,
    val sni: String? = null,
    /** 该上游是否走代理。null 表示跟随全局策略。 */
    val viaProxy: Boolean? = null,
    /** nameserver-policy：仅对这些域名使用本上游。 */
    val forDomains: List<String> = emptyList(),
)

@Serializable
enum class DnsProtocol {
    @SerialName("udp") UDP,
    @SerialName("tcp") TCP,
    @SerialName("doh") DOH,
    @SerialName("doh3") DOH3,
    @SerialName("dot") DOT,
    @SerialName("doq") DOQ,
}

@Serializable
enum class DnsMode {
    @SerialName("normal") NORMAL,
    @SerialName("fake-ip") FAKE_IP,
    @SerialName("hosts") HOSTS,
    @SerialName("redir-host") REDIR_HOST,
}

/** fallback-filter（5.5）。 */
@Serializable
data class FallbackFilter(
    val geoip: Boolean = false,
    val geoipCode: String = "CN",
    val ipCidrs: List<String> = emptyList(),
    val domains: List<String> = emptyList(),
)

/**
 * 完整配置包，用于导入导出。
 *
 * [rawExtras] 承载"未知字段保留"：导入时把无法映射的顶层键原样存下来，
 * 再导出时写回。这样旧版本 App 打开新版本配置不会丢数据（第 10 部分前向兼容要求）。
 */
@Serializable
data class ConfigBundle(
    val schemaVersion: Int = SCHEMA_VERSION,
    val profile: Profile? = null,
    val nodes: List<ProxyNode> = emptyList(),
    val groups: List<ProxyGroup> = emptyList(),
    val ruleSets: List<RuleSet> = emptyList(),
    val rules: List<RuleEntry> = emptyList(),
    val scripts: List<ScriptEntry> = emptyList(),
    val rewrites: List<RewriteEntry> = emptyList(),
    val filters: List<FilterConfig> = emptyList(),
    val rawExtras: Map<String, String> = emptyMap(),
)
