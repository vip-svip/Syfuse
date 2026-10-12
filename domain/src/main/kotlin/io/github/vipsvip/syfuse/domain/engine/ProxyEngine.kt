package io.github.vipsvip.syfuse.domain.engine

import io.github.vipsvip.syfuse.core.result.SyfuseResult
import kotlinx.coroutines.flow.Flow

/**
 * 代理内核抽象接口 —— 本项目「内核可切换」能力的唯一支点。
 *
 * 为什么必须有这一层（提示词 9.2）：
 * UI 层若直接依赖 sing-box 的具体类型，将来接入第二个内核就要改遍全项目。
 * 这里把内核能力收敛为 7 个操作，UI 与 ViewModel 只认这个接口。
 *
 * 已确认决策（M0 §11.3 C2）：M1 只提供 [SingBoxEngine] 一个实现，
 * `MihomoEngine` 排入 v2；但接口从 M1 起即为双内核设计，届时无需改动上层。
 *
 * 实现方约定：
 * - 所有方法必须在 [stop] 之后仍可再次 [start]（幂等可重入）
 * - [stats] 与 [connections] 是热流，停止后应发射空值而非抛异常
 */
interface ProxyEngine {

    /** 内核标识，用于设置页展示与配置编译分派。 */
    val kind: EngineKind

    /** 内核版本号，来自内核自身上报，用于诊断页与更新比对。 */
    suspend fun version(): String

    /**
     * 启动内核。
     *
     * 失败原因常见为：配置非法、端口被占、TUN 建立失败（提示词红队自审第 2 问的
     * 首要根因）、无 VPN 授权。失败必须带可定位的原因，不允许只返回「启动失败」。
     */
    suspend fun start(config: EngineConfig): SyfuseResult<Unit>

    /** 优雅停止，释放 TUN 与监听端口。 */
    suspend fun stop(): SyfuseResult<Unit>

    /**
     * 热重载配置，不中断已有连接。
     *
     * 提示词 6.7 要求「外部改配置文件后自动生效，无需重启」，这依赖本方法。
     * 若内核不支持真正的热重载，实现方应做「增量 diff + 仅重建受影响监听器」，
     * 并在 KDoc 中如实说明降级程度。
     */
    suspend fun reload(config: EngineConfig): SyfuseResult<Unit>

    /** 实时上下行与累计流量。 */
    fun stats(): Flow<TrafficStats>

    /** 活动连接快照流，用于连接列表页。 */
    fun connections(): Flow<List<ConnSnapshot>>

    /** 在策略组中选定成员（提示词 5.2 select 组）。 */
    suspend fun select(group: String, member: String): SyfuseResult<Unit>

    /** 对策略组或单个节点做延迟测速。 */
    suspend fun urlTest(target: String): SyfuseResult<List<LatencyResult>>
}

/** 内核种类。新增内核只需在此加枚举 + 一个实现类。 */
enum class EngineKind(val displayName: String) {
    /** sing-box：协议覆盖最全，M1–M12 的默认与唯一内实现。 */
    SING_BOX("sing-box"),

    /** mihomo：MIT 许可，规则生态成熟，排入 v2。 */
    MIHOMO("mihomo"),
}

/**
 * 运行时配置。
 *
 * 保持为不透明字符串载体：不同内核的配置格式不同（sing-box 用 JSON、mihomo 用
 * Clash YAML），由各自的 [EngineConfigCompiler] 负责编译，接口层不感知格式差异。
 */
data class EngineConfig(
    /** 配置文件绝对路径，内核进程直接读取。 */
    val configPath: String,
    /** 工作目录，用于存放缓存、fake-ip 持久化等。 */
    val workDir: String,
    /** 运行时模式，决定 TUN 还是 TPROXY 通路。 */
    val mode: RunMode,
    /** 日志级别，对应设置页「实验室 → 内核日志级别」。 */
    val logLevel: LogLevel = LogLevel.WARN,
)

/** 三种运行模式（提示词 9.3）。 */
enum class RunMode {
    /** VPN 模式：VpnService + TUN，免 root。 */
    VPN,

    /** root 模式：nftables TPROXY + fwmark。 */
    ROOT_TPROXY,

    /** 仅本机代理，不接管全局流量（用于测试与局域网共享）。 */
    LOCAL_PROXY,
}

/** 内核日志级别。 */
enum class LogLevel { TRACE, DEBUG, INFO, WARN, ERROR, FATAL }

/** 实时流量统计。 */
data class TrafficStats(
    val upBytesPerSec: Long = 0,
    val downBytesPerSec: Long = 0,
    val totalUpBytes: Long = 0,
    val totalDownBytes: Long = 0,
    val activeConnections: Int = 0,
)

/** 一条活动连接。 */
data class ConnSnapshot(
    val id: String,
    val network: String,
    val type: String,
    val srcApp: String?,
    val dstHost: String,
    val dstIp: String?,
    val ruleText: String,
    val chain: List<String>,
    val upBytes: Long,
    val downBytes: Long,
    val startAtMillis: Long,
    val state: ConnState,
)

/** 连接状态。 */
enum class ConnState { ACTIVE, CLOSED, FAILED }

/** 单次测速结果。 */
data class LatencyResult(
    val target: String,
    val delayMillis: Long,
    /** 测速方式，决定延迟口径（提示词 5.2 要求可区分 TCP/TLS/HEAD/ICMP）。 */
    val probe: ProbeKind,
    val error: String? = null,
)

/** 测速方式。 */
enum class ProbeKind { TCP_HANDSHAKE, TLS_HANDSHAKE, HTTP_HEAD, ICMP }

/**
 * 配置编译器：把领域模型转成具体内核的配置文件。
 *
 * 每个内核一个实现。这样「双内核」的差异被完全限制在本接口的实现类里，
 * 不会渗透到 ViewModel 或 UI。
 */
interface EngineConfigCompiler {
    val kind: EngineKind

    /** 编译为配置文件文本。失败时返回带行号的 [io.github.vipsvip.syfuse.core.result.SyfuseError.Parse]。 */
    fun compile(config: EngineRuntimeConfig): SyfuseResult<String>
}

/** 编译期的运行时配置输入（领域模型的子集，只含内核真正需要的字段）。 */
data class EngineRuntimeConfig(
    val mode: RunMode,
    val logLevel: LogLevel,
    /** 已解析的节点列表（内核无关表示）。 */
    val outbounds: List<OutboundSpec>,
    /** 策略组。 */
    val groups: List<GroupSpec>,
    /** 路由规则（已按优先级排好序）。 */
    val rules: List<RouteRuleSpec>,
    /** DNS 配置。 */
    val dns: DnsSpec,
    /** 入站监听配置。 */
    val inbounds: List<InboundSpec>,
    /** 原始用户配置（用于透传内核特有字段，保证不因抽象层丢失能力）。 */
    val rawExtras: Map<String, String> = emptyMap(),
)

/** 出站（节点）的抽象描述。 */
data class OutboundSpec(
    val tag: String,
    val type: String,
    val server: String,
    val port: Int,
    /** 内核特有字段原样透传，避免抽象层削掉协议细节。 */
    val raw: Map<String, String>,
)

/** 策略组抽象描述。 */
data class GroupSpec(
    val tag: String,
    val type: String,
    val members: List<String>,
    val url: String? = null,
    val intervalSeconds: Int? = null,
    val toleranceMillis: Int? = null,
)

/** 路由规则抽象描述。 */
data class RouteRuleSpec(
    val kind: String,
    val payload: String,
    val action: String,
    val extras: Map<String, String> = emptyMap(),
)

/** DNS 抽象描述。 */
data class DnsSpec(
    val servers: List<String>,
    val mode: String,
    val fakeIpPool: String? = null,
    val hosts: Map<String, String> = emptyMap(),
)

/** 入站抽象描述。 */
data class InboundSpec(
    val type: String,
    val listen: String,
    val port: Int,
    val options: Map<String, String> = emptyMap(),
)
