package io.github.vipsvip.syfuse.domain.model

/**
 * 领域模型。
 *
 * 全部字段与提示词第 10 部分的数据模型一一对应。这些类型是「内核无关」的：
 * 它们描述用户意图（我要连哪个节点、走哪条规则），而非某个内核的配置语法。
 */

/** 配置档案（提示词 10 Profile）。 */
data class Profile(
    val id: String,
    val name: String,
    val type: ProfileType,
    val sourceUrl: String? = null,
    /** 自动更新间隔（小时），null 表示不自动更新。 */
    val updateIntervalHours: Int? = null,
    val subscriptionInfo: SubscriptionInfo? = null,
    val rawConfig: String? = null,
    val routingMode: RoutingMode = RoutingMode.RULE,
    val ruleSetIds: List<String> = emptyList(),
    val dnsConfig: DnsConfig? = null,
    val scriptIds: List<String> = emptyList(),
    val createdAtMillis: Long,
    val updatedAtMillis: Long,
    val schemaVersion: Int,
)

/** 档案来源。 */
enum class ProfileType { REMOTE, LOCAL }

/**
 * 全局路由模式 —— 命名必须保留「配置 / 代理 / 直连」（提示词 5.1 明确要求）。
 * 这是 P0 主干的一部分，改名会破坏 Shadowrocket 的概念同名性。
 */
enum class RoutingMode(val displayName: String) {
    /** 配置：按规则分流（默认）。 */
    RULE("配置"),

    /** 代理：全部走代理。 */
    GLOBAL("代理"),

    /** 直连：全部不走代理。 */
    DIRECT("直连"),
}

/** 订阅信息（流量与到期时间）。 */
data class SubscriptionInfo(
    val uploadBytes: Long = 0,
    val downloadBytes: Long = 0,
    val totalBytes: Long = 0,
    val expireAtMillis: Long? = null,
)

/** 代理节点（提示词 10 ProxyNode）。 */
data class ProxyNode(
    val id: String,
    val name: String,
    val protocol: Protocol,
    val server: String,
    val port: Int,
    /** 凭据（密码 / UUID / 私钥），加密存储，不落明文（红线条 R5）。 */
    val auth: NodeAuth,
    val tls: TlsConfig? = null,
    val transport: TransportConfig? = null,
    val plugin: String? = null,
    val tags: List<String> = emptyList(),
    val latencyMillis: Long? = null,
    val favorite: Boolean = false,
    val sortIndex: Int = 0,
    /** 原始节点 JSON，保证导出时字段级等值（提示词 5.2 验收要求）。 */
    val rawJson: String? = null,
)

/** 支持的协议。覆盖提示词 5.2 的全部清单。 */
enum class Protocol(val displayName: String) {
    SS("Shadowsocks"),
    SSR("ShadowsocksR"),
    VMESS("VMess"),
    VLESS("VLESS"),
    TROJAN("Trojan"),
    TROJAN_GO("Trojan-Go"),
    HYSTERIA("Hysteria"),
    HYSTERIA2("Hysteria2"),
    TUIC("TUIC v5"),
    WIREGUARD("WireGuard"),
    SNELL("Snell"),
    HTTP("HTTP"),
    HTTPS("HTTPS"),
    SOCKS5("SOCKS5"),
    SSH("SSH"),
    ANYTLS("AnyTLS"),
    SHADOWTLS("ShadowTLS"),
    JUICITY("Juicity"),
    MIERU("Mieru"),
}

/**
 * 节点凭据。
 *
 * 刻意设计为密封类型而非裸 String：这样序列化时能集中走加密路径，
 * 降低「某处忘了加密」的风险（红线条 R5）。
 */
sealed interface NodeAuth {
    data object None : NodeAuth
    data class Password(val value: String) : NodeAuth
    data class Uuid(val value: String) : NodeAuth
    data class UserPassword(val user: String, val password: String) : NodeAuth
    /** WireGuard 类多密钥凭据。 */
    data class KeyPair(val privateKey: String, val publicKey: String, val presharedKey: String? = null) : NodeAuth
}

/** TLS 参数（提示词 5.2 要求全开放）。 */
data class TlsConfig(
    val enabled: Boolean = true,
    val sni: String? = null,
    val alpn: List<String> = emptyList(),
    /** uTLS 指纹：chrome / firefox / safari / ios / random / 自定义。 */
    val fingerprint: String? = null,
    val skipCertVerify: Boolean = false,
    /** 证书固定（pinning）。 */
    val pinnedCertSha256: List<String> = emptyList(),
    val realityPublicKey: String? = null,
    val realityShortId: String? = null,
)

/** 传输层参数。 */
data class TransportConfig(
    val type: TransportType = TransportType.TCP,
    val path: String? = null,
    val host: String? = null,
    val serviceName: String? = null,
    /** mKCP 相关。 */
    val mKcpSeed: String? = null,
    val headerType: String? = null,
)

/** 传输类型。 */
enum class TransportType(val displayName: String) {
    TCP("TCP"),
    WS("WebSocket"),
    HTTP_UPGRADE("HTTP-Upgrade"),
    GRPC("gRPC"),
    HTTP2("HTTP/2"),
    QUIC("QUIC"),
    MKCP("mKCP"),
    HTTP_OBFS("HTTP-Obfs"),
}

/** 策略组（提示词 10 ProxyGroup）。 */
data class ProxyGroup(
    val id: String,
    val name: String,
    val type: GroupType,
    val members: List<String> = emptyList(),
    val url: String? = null,
    val testIntervalSeconds: Int? = null,
    val toleranceMillis: Int? = null,
    val nested: Boolean = false,
)

/** 策略组类型（提示词 5.2）。 */
enum class GroupType(val displayName: String) {
    SELECT("手动选择"),
    URL_TEST("自动测速"),
    FALLBACK("故障转移"),
    LOAD_BALANCE("负载均衡"),
    RELAY("链式代理"),
}

/** 规则集（提示词 10 RuleSet）。 */
data class RuleSet(
    val id: String,
    val name: String,
    val sourceUrl: String? = null,
    val localPath: String? = null,
    val format: RuleSetFormat,
    val updateIntervalHours: Int? = null,
    val ruleCount: Int = 0,
    val lastUpdatedMillis: Long? = null,
    val hash: String? = null,
    val enabled: Boolean = true,
)

/**
 * 规则集格式。
 *
 * 注意 [MRS] 是 mihomo 的二进制格式、[SRS] 是 sing-box 的格式。
 * 两者都保留是因为双内核方案（M0 §11.3 C2）在 v2 需要同时消费；M1 先支持 [SRS]。
 */
enum class RuleSetFormat(val displayName: String) {
    DOMAIN("domain"),
    IPCIDR("ipcidr"),
    CLASSICAL("classical"),
    MRS("mrs"),
    SRS("srs"),
}

/** 规则条目（提示词 10 RuleEntry）。 */
data class RuleEntry(
    val id: String,
    val sourceId: String,
    val rawText: String,
    val kind: RuleKind,
    val action: String,
    val modifiers: Map<String, String> = emptyMap(),
    val lineNo: Int = 0,
    val scope: String? = null,
    val hitCount: Long = 0,
    val order: Int = 0,
)

/**
 * 规则种类（提示词 10）。
 *
 * [FILTER] 是差异增强层的广告过滤规则（提示词 6.1），它与 [ROUTE] 共用优先级模型
 * 但语义不同：ROUTE 决定走哪条策略，FILTER 决定拦不拦。
 */
enum class RuleKind { FILTER, ROUTE, COSMETIC, HOSTS, BLOCK }

/** DNS 配置（提示词 10 DnsConfig）。 */
data class DnsConfig(
    val servers: List<String> = emptyList(),
    val fallback: List<String> = emptyList(),
    val fallbackFilter: Map<String, String> = emptyMap(),
    val fakeIpPool: String? = null,
    val hosts: Map<String, String> = emptyMap(),
    val cacheSize: Int = 4096,
    val strategy: String = "prefer_ipv4",
    val respectRules: Boolean = true,
    val sniff: Boolean = true,
)

/** 脚本条目（提示词 10 ScriptEntry）。 */
data class ScriptEntry(
    val id: String,
    val name: String,
    val content: String,
    val hooks: List<ScriptHook> = emptyList(),
    val argument: String? = null,
    val enabled: Boolean = true,
    val order: Int = 0,
    val lastRunStatus: String? = null,
    val lastRunAtMillis: Long? = null,
)

/** 脚本 Hook 点（提示词 5.6）。 */
enum class ScriptHook(val displayName: String) {
    HTTP_REQUEST("http-request"),
    HTTP_RESPONSE("http-response"),
    RULE("rule"),
    DNS("dns"),
    CRON("cron"),
}

/** URL 重写条目（提示词 10 RewriteEntry）。 */
data class RewriteEntry(
    val id: String,
    val match: String,
    val actionType: RewriteAction,
    val actionValue: String? = null,
    val enabled: Boolean = true,
)

/** 重写动作（提示词 5.6）。 */
enum class RewriteAction(val displayName: String) {
    REJECT("reject"),
    REJECT_200("reject-200"),
    REJECT_DICT("reject-dict"),
    REJECT_ARRAY("reject-array"),
    REDIRECT_302("302"),
    REDIRECT_307("307"),
    MODIFY_REQUEST_HEADER("请求头改写"),
    MODIFY_RESPONSE_HEADER("响应头改写"),
    MODIFY_REQUEST_BODY("请求体正则替换"),
    MODIFY_RESPONSE_BODY("响应体正则替换"),
}

/** MITM 配置（提示词 10 MitmConfig）。 */
data class MitmConfig(
    val enabled: Boolean = false,
    val hostnames: List<String> = emptyList(),
    val caFingerprint: String? = null,
    val caInstalled: Boolean = false,
    val httpPort: Int = 8080,
    val httpsPort: Int = 8443,
    val pinningPolicy: String = "strict",
)

/** 过滤配置（提示词 10 FilterConfig）。 */
data class FilterConfig(
    val id: String,
    val name: String,
    /** 规则源（本地路径或远程 URL）。 */
    val sources: List<String> = emptyList(),
    val enabled: Boolean = true,
    val updateIntervalHours: Int? = null,
    val priority: Int = 0,
)

/** 开屏跳过规则（提示词 10 SplashSkipRule，差异增强层 6.2）。 */
data class SplashSkipRule(
    val id: String,
    val packageName: String,
    val matchType: String,
    val matchValue: String,
    val action: SplashAction,
    val delayMillis: Long = 500,
    val cooldownMillis: Long = 3000,
    val enabled: Boolean = false,
    val hitCount: Long = 0,
)

/** 开屏跳过动作。 */
enum class SplashAction(val displayName: String) {
    CLICK_SKIP("点击跳过"),
    FINISH_ACTIVITY("直接结束"),
    SWIPE("滑动跳过"),
}

/** 通知净化规则（提示词 10 NotifyRule，差异增强层 6.3）。 */
data class NotifyRule(
    val id: String,
    val packageName: String? = null,
    val keyword: String? = null,
    val regex: String? = null,
    val action: NotifyAction = NotifyAction.FOLD,
    val enabled: Boolean = false,
)

/** 通知处理动作。 */
enum class NotifyAction(val displayName: String) {
    FOLD("仅折叠"),
    BLOCK("拦截"),
}

/** 特权能力状态（提示词 10 PrivilegeState）。 */
data class PrivilegeState(
    val rootProvider: RootProvider = RootProvider.NONE,
    val rootGranted: Boolean = false,
    val lsposedActive: Boolean = false,
    val moduleScope: List<String> = emptyList(),
    val systemCaWritable: Boolean = false,
    val nftablesAvailable: Boolean = false,
    val tproxyAvailable: Boolean = false,
    val overlayfsAvailable: Boolean = false,
)

/** root 提供方（提示词 6.5）。 */
enum class RootProvider(val displayName: String) {
    NONE("无"),
    KERNELSU("KernelSU"),
    MAGISK("Magisk"),
    APATCH("APatch"),
}

/**
 * 已安装改动清单（提示词 10 InstalledManifest，侵入原则 6.9）。
 *
 * 每一项系统级改动都必须可还原，这是红线条 R6 的落地载体。
 */
data class InstalledManifest(
    val entryId: String,
    val kind: ManifestKind,
    val target: String,
    val source: String,
    val restorable: Boolean,
    val createdAtMillis: Long,
)

/** 改动种类。 */
enum class ManifestKind { MOUNT, CERT, HOSTS, MODULE }

/** 应用设置（提示词 10 AppSettings）。 */
data class AppSettings(
    val theme: ThemeMode = ThemeMode.SYSTEM,
    val dynamicColor: Boolean = true,
    val language: String = "auto",
    val expertMode: Boolean = false,
    val autostart: Boolean = false,
    val tileBehavior: String = "toggle",
    val apiToken: String? = null,
    /** 零遥测：恒为 false，不可由用户或代码开启（红线条 R3）。 */
    val telemetry: Boolean = false,
)

/** 主题模式（提示词 5.10：浅色 / 深色 / 跟随系统 / AMOLED 纯黑）。 */
enum class ThemeMode(val displayName: String) {
    LIGHT("浅色"),
    DARK("深色"),
    SYSTEM("跟随系统"),
    AMOLED("AMOLED 纯黑"),
}
