package io.github.vipsvip.syfuse.domain.settings

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** 主题模式（5.10 通用分组）。 */
@Serializable
enum class ThemeMode {
    @SerialName("light") LIGHT,
    @SerialName("dark") DARK,
    @SerialName("system") SYSTEM,
    @SerialName("amoled") AMOLED,
}

/**
 * 风险等级。7.3 要求每个设置项都用三色标记。
 * 判定标准（避免主观）：
 *  - SAFE：改了只影响观感或可随时改回，不改变网络行为；
 *  - ADVANCED：改变网络行为或资源占用，但可逆且无残留；
 *  - DANGEROUS：涉及系统分区、证书、无障碍/通知监听、脚本执行，需要二次确认与还原路径。
 */
enum class RiskLevel {
    SAFE,
    ADVANCED,
    DANGEROUS,
}

/** 设置项控件类型。 */
enum class SettingType {
    TOGGLE,
    TEXT,
    NUMBER,
    ENUM,
    ACTION,
}

/** 设置分组。分组名与 5.10 完全一致。 */
enum class SettingGroup(val displayName: String) {
    GENERAL("通用"),
    NETWORK_DNS("网络与 DNS"),
    TUNNEL("隧道与内核"),
    HTTPS_DECRYPT("HTTPS 解密"),
    SCRIPT_REWRITE("脚本与重写"),
    SUBSCRIPTION_BACKUP("订阅与备份"),
    RULES("规则"),
    STATS_PRIVACY("统计与隐私"),
    PRIVILEGE_ENHANCE("特权与增强"),
    LAB("实验室"),
    ABOUT("关于"),
}

/**
 * 单个设置项的定义。5.10 要求每项都必须有
 * 标题 + 副标题说明 + 风险等级 + 恢复推荐值。
 * 把定义做成数据（而不是散落在 UI 代码里），才能同时满足：
 * 设置搜索、专家模式统一揭示、恢复推荐值、以及"配置即产品"的双向同步（6.8）。
 */
data class SettingSpec(
    val key: String,
    val group: SettingGroup,
    val title: String,
    val subtitle: String,
    val risk: RiskLevel,
    val type: SettingType,
    val defaultValue: String,
    val enumOptions: List<String> = emptyList(),
    val range: IntRange? = null,
    val unit: String? = null,
    /** 是否仅在专家模式下可见（5.10：高级项默认隐藏）。 */
    val expertOnly: Boolean = false,
    /** 是否属于"需要二次确认"的危险动作。 */
    val requiresConfirm: Boolean = false,
)

/** 持久化的设置值。键值形态让新增设置项不需要改 schema（前向兼容）。 */
@Serializable
data class AppSettings(
    val values: Map<String, String> = emptyMap(),
    /** R3：遥测永远为 false，且不提供开启入口。此处仅为显式声明，不参与读写。 */
    val telemetry: Boolean = false,
) {
    fun get(key: String): String? = values[key]

    fun getBoolean(key: String): Boolean = values[key] == "true"

    fun getInt(key: String): Int? = values[key]?.toIntOrNull()

    operator fun plus(pair: Pair<String, String>): AppSettings =
        copy(values = values + pair)

    fun withValue(key: String, value: String): AppSettings = copy(values = values + (key to value))

    fun without(key: String): AppSettings = copy(values = values - key)
}

/**
 * 设置目录。按 5.10 的分组结构定义全部设置项。
 *
 * 注意：这里只声明"有哪些设置、默认值是什么、风险多高"。
 * 具体某个值如何作用于内核，由 engine 层按 key 消费——目录本身不含行为。
 */
object SettingsCatalog {

    private fun s(
        key: String,
        group: SettingGroup,
        title: String,
        subtitle: String,
        risk: RiskLevel = RiskLevel.SAFE,
        type: SettingType = SettingType.TOGGLE,
        default: String = "false",
        options: List<String> = emptyList(),
        range: IntRange? = null,
        unit: String? = null,
        expertOnly: Boolean = false,
        confirm: Boolean = false,
    ) = SettingSpec(key, group, title, subtitle, risk, type, default, options, range, unit, expertOnly, confirm)

    val all: List<SettingSpec> = listOf(
        // ---------------- 通用 ----------------
        s("theme", SettingGroup.GENERAL, "主题", "浅色 / 深色 / 跟随系统 / AMOLED 纯黑", type = SettingType.ENUM, default = "system", options = listOf("light", "dark", "system", "amoled")),
        s("dynamic_color", SettingGroup.GENERAL, "动态取色", "使用系统壁纸取色；系统不支持时回退内置种子色"),
        s("language", SettingGroup.GENERAL, "语言", "界面语言；默认跟随系统", type = SettingType.ENUM, default = "system", options = listOf("system", "zh-CN", "en-US")),
        s("expert_mode", SettingGroup.GENERAL, "专家模式", "揭示全部高级设置项。关闭时高级项在列表中隐藏"),
        s("startup_behavior", SettingGroup.GENERAL, "启动行为", "应用启动时是否自动恢复上次连接", type = SettingType.ENUM, default = "restore_last", options = listOf("restore_last", "always_connect", "never_connect")),

        // ---------------- 网络与 DNS ----------------
        s("dns_mode", SettingGroup.NETWORK_DNS, "DNS 模式", "normal / fake-ip / hosts / redir-host", risk = RiskLevel.ADVANCED, type = SettingType.ENUM, default = "normal", options = listOf("normal", "fake-ip", "hosts", "redir-host")),
        s("dns_upstream", SettingGroup.NETWORK_DNS, "DNS 上游", "主上游地址，支持 UDP / DoH / DoT / DoQ", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = "1.1.1.1"),
        s("dns_upstream_enc", SettingGroup.NETWORK_DNS, "加密上游", "DoH / DoT / DoQ 地址；留空则只使用明文上游", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = ""),
        s("dns_fallback", SettingGroup.NETWORK_DNS, "回退上游", "主上游失败时使用的上游", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = ""),
        s("dns_fake_ip_pool", SettingGroup.NETWORK_DNS, "fake-ip 地址池", "fake-ip 模式的地址池，需为保留网段", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = "198.18.0.0/15"),
        s("dns_cache_size", SettingGroup.NETWORK_DNS, "DNS 缓存条数", "LRU 缓存容量；0 表示关闭缓存", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "4096", range = 0..65535, expertOnly = true),
        s("dns_sniff", SettingGroup.NETWORK_DNS, "域名嗅探", "从 TLS SNI / HTTP Host / QUIC 中提取域名"),
        s("dns_respect_rules", SettingGroup.NETWORK_DNS, "DNS 遵守分流规则", "DNS 请求按分流规则决定直连或走代理", risk = RiskLevel.ADVANCED),
        s("dns_leak_protect", SettingGroup.NETWORK_DNS, "DNS 防泄漏", "强制 DNS 请求走隧道，避免明文泄漏", risk = RiskLevel.ADVANCED),
        s("proxy_port", SettingGroup.NETWORK_DNS, "本地代理端口", "供局域网内其它设备使用的 HTTP/SOCKS 入口端口", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "2080", range = 1024..65535),
        s("lan_access", SettingGroup.NETWORK_DNS, "允许局域网访问", "开放本地代理端口给局域网设备", risk = RiskLevel.ADVANCED),

        // ---------------- 隧道与内核 ----------------
        s("tun_mtu", SettingGroup.TUNNEL, "MTU", "隧道最大传输单元；过大易分片，过小影响吞吐", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "1500", range = 576..9000, unit = "字节"),
        s("tun_stack", SettingGroup.TUNNEL, "TUN 栈", "gvisor 兼容性最好；system 性能更高；mixed 自动择优", risk = RiskLevel.ADVANCED, type = SettingType.ENUM, default = "gvisor", options = listOf("gvisor", "system", "mixed")),
        s("tun_auto_route", SettingGroup.TUNNEL, "auto-route", "自动接管系统路由表", risk = RiskLevel.ADVANCED),
        s("tun_strict_route", SettingGroup.TUNNEL, "strict-route", "严格路由：阻断未走隧道的流量，可防止部分泄漏", risk = RiskLevel.ADVANCED, expertOnly = true),
        s("tcp_congestion", SettingGroup.TUNNEL, "拥塞控制算法", "如 cubic / bbr；内核不支持时自动回退", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = "", expertOnly = true),
        s("tcp_fast_open", SettingGroup.TUNNEL, "TCP Fast Open", "减少握手往返；部分网络环境下会失败", risk = RiskLevel.ADVANCED, expertOnly = true),
        s("mux_type", SettingGroup.TUNNEL, "多路复用类型", "h2mux / smux / yamux；空表示关闭", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = "", expertOnly = true),
        s("mux_concurrency", SettingGroup.TUNNEL, "多路复用并发", "并发流上限", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "8", range = 1..128, expertOnly = true),
        s("udp_relay_mode", SettingGroup.TUNNEL, "UDP 中继模式", "native / quic / 关闭", risk = RiskLevel.ADVANCED, type = SettingType.ENUM, default = "native", options = listOf("native", "quic", "off"), expertOnly = true),
        s("buffer_size", SettingGroup.TUNNEL, "缓冲区大小", "单连接读写缓冲；过大增加内存占用", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "4096", range = 1024..65536, unit = "字节", expertOnly = true),

        // ---------------- HTTPS 解密 ----------------
        s("mitm_enable", SettingGroup.HTTPS_DECRYPT, "HTTPS 解密（MITM）", "解密指定域名的 HTTPS 流量以实现重写与过滤。仅对你拥有授权的流量使用", risk = RiskLevel.DANGEROUS, default = "false", confirm = true),
        s("mitm_hostnames", SettingGroup.HTTPS_DECRYPT, "解密域名", "逗号分隔，支持通配符；留空表示不解密任何域名", risk = RiskLevel.DANGEROUS, type = SettingType.TEXT, default = ""),
        s("mitm_https_port", SettingGroup.HTTPS_DECRYPT, "HTTPS 上游端口", "解密后回连上游使用的端口", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "8443", range = 1..65535),
        s("mitm_http_port", SettingGroup.HTTPS_DECRYPT, "HTTP 上游端口", "HTTP 上游端口", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "8080", range = 1..65535),
        s("mitm_pinning", SettingGroup.HTTPS_DECRYPT, "证书固定策略", "passthrough 遇到固定证书直接直通；ignore 忽略校验（不推荐）", risk = RiskLevel.DANGEROUS, type = SettingType.ENUM, default = "passthrough", options = listOf("passthrough", "verify", "ignore")),
        s("mitm_ca_manage", SettingGroup.HTTPS_DECRYPT, "CA 证书管理", "生成 / 导出 / 安装 / 卸载 CA。卸载必须能完全还原", risk = RiskLevel.DANGEROUS, type = SettingType.ACTION, confirm = true),

        // ---------------- 脚本与重写 ----------------
        s("script_enable", SettingGroup.SCRIPT_REWRITE, "启用脚本", "允许执行 JS 脚本处理请求与响应", risk = RiskLevel.DANGEROUS, confirm = true),
        s("script_timeout_ms", SettingGroup.SCRIPT_REWRITE, "脚本超时", "单次执行超时，超时即中止", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "5000", range = 100..60000, unit = "毫秒"),
        s("script_log", SettingGroup.SCRIPT_REWRITE, "脚本日志", "记录脚本执行日志，便于调试"),
        s("rewrite_enable", SettingGroup.SCRIPT_REWRITE, "启用 URL 重写", "启用 reject / 302 / 头改写 / 正文替换", risk = RiskLevel.ADVANCED),

        // ---------------- 订阅与备份 ----------------
        s("sub_auto_update", SettingGroup.SUBSCRIPTION_BACKUP, "订阅自动更新", "按间隔自动更新远程订阅"),
        s("sub_update_interval", SettingGroup.SUBSCRIPTION_BACKUP, "订阅更新间隔", "自动更新间隔", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "24", range = 1..720, unit = "小时"),
        s("sub_wifi_only", SettingGroup.SUBSCRIPTION_BACKUP, "仅 Wi-Fi 更新", "移动网络下不自动更新订阅，避免消耗流量"),
        s("sub_diff_preview", SettingGroup.SUBSCRIPTION_BACKUP, "更新前差异预览", "覆盖前显示新增 / 删除 / 改名节点数"),
        s("local_encrypted_backup", SettingGroup.SUBSCRIPTION_BACKUP, "本地加密备份", "导出配置为加密包；口令不落盘", risk = RiskLevel.ADVANCED, type = SettingType.ACTION),
        s("webdav_backup", SettingGroup.SUBSCRIPTION_BACKUP, "WebDAV 备份", "可选：把加密备份上传到你自己的 WebDAV 空间", risk = RiskLevel.ADVANCED, type = SettingType.ACTION),

        // ---------------- 规则 ----------------
        s("rule_source_manage", SettingGroup.RULES, "规则源管理", "本地文件或远程 URL、更新间隔、镜像加速", risk = RiskLevel.ADVANCED, type = SettingType.ACTION),
        s("ruleset_auto_update", SettingGroup.RULES, "规则集自动更新", "按间隔更新远程规则集并校验哈希"),
        s("ruleset_mirror", SettingGroup.RULES, "规则源镜像", "规则库下载镜像前缀，留空表示直连", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = ""),
        s("rule_health_check", SettingGroup.RULES, "规则体检", "检查语法错误、冲突、永不命中的规则", type = SettingType.ACTION),
        s("rule_tester", SettingGroup.RULES, "规则测试", "输入域名 / IP / 应用 / 端口，输出完整匹配链路", type = SettingType.ACTION),
        s("engine_heuristic", SettingGroup.RULES, "启发式域名提取", "从含路径的规则中提取域名参与 DNS 层拦截", risk = RiskLevel.ADVANCED, expertOnly = true),
        s("engine_strict_scope", SettingGroup.RULES, "作用域严格模式", "开启后仅当客户端标签匹配时 \$client / \$ctag 规则才生效", risk = RiskLevel.ADVANCED, expertOnly = true),
        s("engine_client", SettingGroup.RULES, "本机客户端标识", "对应规则中的 \$client=", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = "", expertOnly = true),
        s("engine_ctag", SettingGroup.RULES, "本机标签组", "对应规则中的 \$ctag=", risk = RiskLevel.ADVANCED, type = SettingType.TEXT, default = "", expertOnly = true),

        // ---------------- 统计与隐私 ----------------
        s("stats_enable", SettingGroup.STATS_PRIVACY, "启用统计", "记录会话与分应用流量统计"),
        s("stats_retention_days", SettingGroup.STATS_PRIVACY, "数据保留期", "超期统计自动清理；0 表示不保留", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "7", range = 0..365, unit = "天"),
        s("export_stats", SettingGroup.STATS_PRIVACY, "导出统计", "导出为 CSV 或 JSON", type = SettingType.ACTION),
        s("conn_audit_log", SettingGroup.STATS_PRIVACY, "连接级审计日志", "记录每条连接的完整链路，便于排查", risk = RiskLevel.ADVANCED),

        // ---------------- 特权与增强 ----------------
        s("root_features", SettingGroup.PRIVILEGE_ENHANCE, "root 能力", "免 VPN 弹窗的透明代理、UID 精确分流。需已安装并授权 KernelSU / Magisk / APatch", risk = RiskLevel.DANGEROUS, type = SettingType.ACTION, confirm = true),
        s("lsposed_hooks", SettingGroup.PRIVILEGE_ENHANCE, "LSPosed 增强", "按应用注入 CA 信任与 DNS/代理覆盖", risk = RiskLevel.DANGEROUS, confirm = true),
        s("ad_filter_enable", SettingGroup.PRIVILEGE_ENHANCE, "广告过滤", "启用融合过滤引擎（DNS 层 + 连接层 + hosts）", risk = RiskLevel.ADVANCED),
        s("net_block_enable", SettingGroup.PRIVILEGE_ENHANCE, "网络层封禁", "按域名 / IP / 网段 / 端口封禁", risk = RiskLevel.ADVANCED),
        s("splash_skip_enable", SettingGroup.PRIVILEGE_ENHANCE, "开屏跳过", "识别并跳过应用开屏广告。无障碍方式需授权无障碍服务，默认关闭", risk = RiskLevel.DANGEROUS, confirm = true),
        s("splash_skip_delay", SettingGroup.PRIVILEGE_ENHANCE, "开屏跳过延迟", "命中后延迟多久执行动作，避免误触", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "800", range = 0..10000, unit = "毫秒", expertOnly = true),
        s("splash_skip_cooldown", SettingGroup.PRIVILEGE_ENHANCE, "开屏跳过冷却", "同一应用两次动作之间的最小间隔", risk = RiskLevel.ADVANCED, type = SettingType.NUMBER, default = "3000", range = 0..60000, unit = "毫秒", expertOnly = true),
        s("notify_clean_enable", SettingGroup.PRIVILEGE_ENHANCE, "通知净化", "按关键字或正则折叠/拦截广告通知。需授权通知使用权，默认关闭", risk = RiskLevel.DANGEROUS, confirm = true),
        s("hosts_inject_enable", SettingGroup.PRIVILEGE_ENHANCE, "hosts 注入", "以 OverlayFS 或 bind mount 注入 hosts，卸载即完全复原", risk = RiskLevel.DANGEROUS, confirm = true),

        // ---------------- 实验室 ----------------
        s("kernel_log_level", SettingGroup.LAB, "内核日志级别", "debug / info / warn / error", risk = RiskLevel.ADVANCED, type = SettingType.ENUM, default = "info", options = listOf("debug", "info", "warn", "error"), expertOnly = true),
        s("pprof_capture", SettingGroup.LAB, "性能采集（pprof）", "采集 CPU / 内存剖面用于定位性能问题", risk = RiskLevel.ADVANCED, type = SettingType.ACTION, expertOnly = true),
        s("local_api_enable", SettingGroup.LAB, "本地 HTTP API", "开放本地控制接口；默认关闭，开启后必须设置令牌", risk = RiskLevel.DANGEROUS, confirm = true),
        s("lan_panel_enable", SettingGroup.LAB, "局域网面板", "仅供桌面浏览器查看与管理；默认关闭，不承载 App 主界面", risk = RiskLevel.DANGEROUS, confirm = true),
        s("config_hot_reload", SettingGroup.LAB, "配置热重载", "外部修改配置文件后自动生效，不中断连接", risk = RiskLevel.ADVANCED),
        s("experimental_features", SettingGroup.LAB, "实验特性开关", "开启尚未稳定的实验功能", risk = RiskLevel.ADVANCED, expertOnly = true),

        // ---------------- 关于 ----------------
        s("check_update", SettingGroup.ABOUT, "检查更新", "检查内核与规则集更新；更新前校验 SHA-256 与签名", type = SettingType.ACTION),
        s("open_source_license", SettingGroup.ABOUT, "开源许可", "GPL-3.0-or-later，含第三方组件清单", type = SettingType.ACTION),
        s("acknowledgements", SettingGroup.ABOUT, "致谢", "过滤规则语义与验证纪律参考 SY-Fusion；代理内核来源见 NOTICE", type = SettingType.ACTION),
        s("disclaimer", SettingGroup.ABOUT, "免责声明", "本工具仅用于合法用途，见 DISCLAIMER.md", type = SettingType.ACTION),
    )

    private val byKey: Map<String, SettingSpec> = all.associateBy { it.key }

    init {
        val duplicates = all.groupingBy { it.key }.eachCount().filterValues { it > 1 }.keys
        check(duplicates.isEmpty()) { "设置目录存在重复 key：$duplicates" }
    }

    fun byKey(key: String): SettingSpec? = byKey[key]

    /** 默认值映射，用于"恢复推荐值"。 */
    fun defaults(): Map<String, String> = all.associate { it.key to it.defaultValue }

    /**
     * 设置搜索。匹配标题、副标题与 key，忽略大小写。
     * 搜索范围包含被专家模式隐藏的项——否则用户搜不到自己知道存在的设置。
     */
    fun search(query: String): List<SettingSpec> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        return all.filter {
            it.title.contains(q, ignoreCase = true) ||
                it.subtitle.contains(q, ignoreCase = true) ||
                it.key.contains(q, ignoreCase = true) ||
                it.group.displayName.contains(q, ignoreCase = true)
        }
    }

    /** 某分组下的可见设置项。[expertMode] 为 false 时隐藏 [SettingSpec.expertOnly]。 */
    fun visibleIn(group: SettingGroup, expertMode: Boolean): List<SettingSpec> =
        all.filter { it.group == group && (expertMode || !it.expertOnly) }

    fun groups(): List<SettingGroup> = SettingGroup.entries.toList()
}
