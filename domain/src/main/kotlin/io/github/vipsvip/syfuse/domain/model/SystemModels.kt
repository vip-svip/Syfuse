package io.github.vipsvip.syfuse.domain.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** root 提供方（6.5）。 */
@Serializable
enum class RootProvider {
    @SerialName("none") NONE,
    @SerialName("kernelsu") KERNELSU,
    @SerialName("magisk") MAGISK,
    @SerialName("apatch") APATCH,
}

/**
 * 特权能力探测结果（6.5 的"能力探测页"数据源）。
 *
 * 每一项都必须是**实测得到**的布尔值，而不是"设备有 root 所以应该都行"的推断。
 * 例如 nftables 可用但内核缺 TPROXY 时，[tproxyAvailable] 必须为 false。
 */
@Serializable
data class PrivilegeState(
    val rootProvider: RootProvider = RootProvider.NONE,
    val rootGranted: Boolean = false,
    val lsposedActive: Boolean = false,
    val moduleScope: List<String> = emptyList(),
    val systemCaWritable: Boolean = false,
    val nftablesAvailable: Boolean = false,
    val tproxyAvailable: Boolean = false,
    val overlayfsAvailable: Boolean = false,
    val moduleInstalled: Boolean = false,
    /** 探测时间戳，UI 必须显示，避免用户看过期结论。 */
    val probedAt: Long = 0L,
)

/** 已安装改动的登记项（6.9 侵入原则的落地载体）。 */
@Serializable
data class InstalledManifest(
    val entryId: String,
    val kind: InstalledKind,
    val target: String,
    val source: String,
    val restorable: Boolean,
    val createdAt: Long = 0L,
)

@Serializable
enum class InstalledKind {
    @SerialName("mount") MOUNT,
    @SerialName("cert") CERT,
    @SerialName("hosts") HOSTS,
    @SerialName("module") MODULE,
}

/** 流量统计采样（5.7）。 */
@Serializable
data class TrafficStat(
    val ts: Long,
    val appUid: Int,
    val packageName: String? = null,
    val host: String? = null,
    val ruleId: String? = null,
    val chain: String? = null,
    val upBytes: Long = 0,
    val downBytes: Long = 0,
    val durationMs: Long = 0,
)

/** 连接快照（5.7 的连接列表）。 */
@Serializable
data class ConnSnapshot(
    val id: String,
    val network: String,
    val type: String,
    val srcApp: String? = null,
    val dstHost: String? = null,
    val dstIp: String? = null,
    val ruleText: String? = null,
    val chain: String? = null,
    val upBytes: Long = 0,
    val downBytes: Long = 0,
    val startAt: Long = 0,
    val state: ConnState = ConnState.ACTIVE,
)

@Serializable
enum class ConnState {
    @SerialName("active") ACTIVE,
    @SerialName("closed") CLOSED,
    @SerialName("failed") FAILED,
}
