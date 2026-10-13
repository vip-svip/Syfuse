package io.github.vipsvip.syfuse.ui.stats

import android.app.Application
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import dagger.hilt.android.EntryPointAccessors
import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonCapability
import io.github.vipsvip.syfuse.daemon.DaemonClient
import io.github.vipsvip.syfuse.ui.components.ErrorState
import io.github.vipsvip.syfuse.ui.components.LoadingState
import io.github.vipsvip.syfuse.ui.components.SectionHeader
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * 统计 页（M9「数据与统计」其三）。
 *
 * **诚实边界（本仓库第一原则）**：
 *
 *  1. 页面只展示**守护进程真实应答**里的字段：`proxies.status`（引擎状态 /
 *     选中节点 / 节点数 / 监听地址 / 运行时长 / 内核接通情况）与 `status`
 *     （守护进程版本 / 协议版本 / 进程运行时长）。没上报的字段显示"未上报"。
 *  2. 累计流量（`traffic`）与活动连接数（`connections`）属 M9，守护进程
 *     尚未实现。这里**如实标注不可用 + 计划里程碑**，不显示 0 ——
 *     0 会被用户误读成"没有流量 / 没有连接"。
 *  3. 网络只走 [DaemonClient]（`EngineModule` 提供的 Singleton），
 *     本文件不自建任何 socket。
 *
 * 关于 [androidx.compose.ui.tooling.preview.Preview]：app 模块编译类路径上
 * 没有 `ui-tooling-preview`（已用 `:app:dependencies --configuration
 * debugCompileClasspath` 实测确认）。内容 composable 因此全部组织为
 * "普通数据类入参 + 无 IO + 无 Context"的纯函数；补上该依赖后即可
 * 机械地外包一层 `@Preview`。
 */

/** Hilt 取件点：从 SingletonComponent 取 [DaemonClient]。 */
@EntryPoint
@InstallIn(SingletonComponent::class)
interface DaemonClientEntryPoint {
    fun daemonClient(): DaemonClient
}

/**
 * 统计页入口。导航只需挂这一行：`composable("stats") { StatsEntry() }`。
 *
 * 为什么不用 `@HiltViewModel` + `hiltViewModel()`：app 模块编译类路径上没有
 * hilt-navigation-compose（实测），ViewModel 装配约定在 `ui` 模块。
 * [DaemonClient] 由 `EngineModule` 以 Singleton 提供，按 Hilt 文档用
 * [EntryPointAccessors] 取它，不改构建文件、不新增依赖。
 */
@Composable
fun StatsEntry(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val client = remember(context) {
        val app = context.applicationContext as Application
        EntryPointAccessors.fromApplication(app, DaemonClientEntryPoint::class.java).daemonClient()
    }
    StatsScreen(client = client, modifier = modifier)
}

/** 统计页状态。 */
private sealed interface StatsUiState {
    data object Loading : StatsUiState

    /** 主数据（proxies.status）不可得：守护进程不可达或应答无法解读。 */
    data class Unavailable(val error: SyfuseError) : StatsUiState

    /**
     * @param daemon 守护进程 `status` 的展示子集；读取失败时为 null（附 [daemonError]）。
     * @param daemonError 守护进程信息读取失败的原因；成功时为 null。
     */
    data class Ready(
        val proxy: ProxyStatus,
        val daemon: DaemonInfo?,
        val daemonError: SyfuseError?,
    ) : StatsUiState
}

/** `proxies.status` 全量展示字段。任一字段都可以是"没上报"。 */
private data class ProxyStatus(
    val state: String?,
    val running: Boolean?,
    val profileId: String?,
    val selected: String?,
    val nodeCount: Int?,
    val uptimeSec: Long?,
    val kernelLinked: Boolean?,
    val socksAddr: String?,
)

/** 守护进程 `status` 的展示子集。 */
private data class DaemonInfo(
    val daemonVersion: String?,
    val protocolVersion: Int?,
    val uptimeSeconds: Long?,
)

@Composable
fun StatsScreen(
    client: DaemonClient,
    modifier: Modifier = Modifier,
) {
    var uiState by remember { mutableStateOf<StatsUiState>(StatsUiState.Loading) }

    var reloadToken by remember { mutableStateOf(0) }
    LaunchedEffect(client, reloadToken) {
        uiState = loadStats(client)
    }

    Column(
        modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SectionHeader("统计")
        when (val state = uiState) {
            StatsUiState.Loading -> LoadingState(message = "正在读取守护进程数据…")

            is StatsUiState.Unavailable -> ErrorState(
                userMessage = state.error.userMessage,
                technical = state.error.technical,
                hint = state.error.hint,
                onRetry = { reloadToken++ },
            )

            is StatsUiState.Ready -> {
                ProxyStatusSection(state.proxy)
                DaemonSection(state.daemon, state.daemonError)
                PendingMetricsSection()
            }
        }
    }
}

/** `proxies.status` 的真实字段。 */
@Composable
private fun ProxyStatusSection(status: ProxyStatus) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                "代理概况（来自 proxies.status）",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            InfoRow("引擎状态", stateLabel(status.state))
            InfoRow(
                "用户已要求启动",
                when (status.running) {
                    true -> "是"
                    false -> "否"
                    null -> "未上报"
                },
            )
            InfoRow(
                "内核宿主",
                when (status.kernelLinked) {
                    true -> "已接通"
                    false -> "未接通 —— 运行时长与选择是真实的，但流量尚未经过代理"
                    null -> "未上报"
                },
            )
            InfoRow("当前选中节点", status.selected?.takeIf { it.isNotBlank() } ?: "未选择")
            InfoRow("配置（profileId）", status.profileId?.takeIf { it.isNotBlank() } ?: "未上报")
            InfoRow("节点数", status.nodeCount?.toString() ?: "未上报")
            InfoRow(
                "SOCKS5 监听地址",
                status.socksAddr ?: "未监听（守护进程未启动隧道时不上报该字段）",
            )
            InfoRow("已运行时长", uptimeLabel(status.uptimeSec))
        }
    }
}

/**
 * 守护进程 `status` 段。
 *
 * 这一段是**附加信息**：读不到时如实给出原因，不影响上方代理概况的展示。
 */
@Composable
private fun DaemonSection(daemon: DaemonInfo?, error: SyfuseError?) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                "守护进程（来自 status）",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            if (daemon == null) {
                InfoRow("版本", "不可用")
                if (error != null) {
                    Text(
                        text = "${error.userMessage}${error.hint?.let { "（$it）" } ?: ""}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                    )
                }
            } else {
                InfoRow("版本", daemon.daemonVersion ?: "未上报")
                InfoRow("协议版本", daemon.protocolVersion?.toString() ?: "未上报")
                InfoRow("进程运行时长", uptimeLabel(daemon.uptimeSeconds))
            }
        }
    }
}

/**
 * 明确标注"不可用"的指标。
 *
 * [DaemonCapability] 是唯一事实来源：里程碑从这里取，方法一旦实现，
 * 文案随之改变 —— 而不是写死一个"即将推出"。
 */
@Composable
private fun PendingMetricsSection() {
    val trafficMilestone = DaemonCapability.pendingMilestone("traffic")
    val connectionsMilestone = DaemonCapability.pendingMilestone("connections")
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                "暂不可用的指标",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            PendingMetricRow("累计流量（traffic）", trafficMilestone)
            PendingMetricRow("活动连接数（connections）", connectionsMilestone)
            Text(
                "这些指标在守护进程侧实现前宁可不显示 —— 显示 0 会被误读成" +
                    "“没有流量 / 没有连接”，那是编造事实。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun PendingMetricRow(label: String, milestone: String?) {
    InfoRow(
        label = label,
        value = when (milestone) {
            null -> "不可用（里程碑未登记）"
            else -> "不可用（计划里程碑 $milestone）"
        },
    )
}

/** 一行"标签 + 值"。与 ui 模块 SettingRow 的三段式保持同一视觉语言。 */
@Composable
private fun InfoRow(label: String, value: String) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .semantics { contentDescription = "$label：$value" },
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(value, style = MaterialTheme.typography.bodyMedium)
    }
}

/** 守护进程原始 state 值 → 可读标签。未收录的值原样显示，不猜。 */
private fun stateLabel(state: String?): String = when (state) {
    "stopped" -> "已停止"
    "starting" -> "启动中"
    "running" -> "运行中"
    "failed" -> "失败"
    "not_linked" -> "未接通（内核宿主未加载）"
    null -> "未上报"
    else -> state
}

/**
 * 秒数 → 时长文本。
 *
 * 0 的语义来自守护进程：只有真正启动过才累计 uptime，所以 0 是
 * "尚未开始计时"，不是"运行了 0 秒"。
 */
private fun uptimeLabel(seconds: Long?): String = when {
    seconds == null -> "未上报"
    seconds == 0L -> "尚未开始计时（未启动过）"
    seconds < 60 -> "$seconds 秒"
    else -> {
        val minutes = seconds / 60
        val hours = minutes / 60
        val days = hours / 24
        when {
            days > 0 -> "$days 天 ${hours % 24} 小时"
            hours > 0 -> "$hours 小时 ${minutes % 60} 分"
            else -> "$minutes 分钟"
        }
    }
}

private suspend fun loadStats(client: DaemonClient): StatsUiState {
    val proxyResult = client.call("proxies.status")
    val proxy = when (proxyResult) {
        is SyfuseResult.Success -> parseProxyStatus(proxyResult.value)
        is SyfuseResult.Failure -> return StatsUiState.Unavailable(proxyResult.error)
    }
    if (proxy == null) {
        return StatsUiState.Unavailable(
            SyfuseError(
                code = ErrorCode.ENGINE_IPC_FAILED,
                technical = "proxies.status 返回非预期格式",
                userMessage = "守护进程返回了无法解读的代理状态",
                hint = "App 与模块版本可能不匹配",
            ),
        )
    }
    // 守护进程信息是附加段：读不到不影响主数据展示，如实降级。
    val daemonResult = client.call("status")
    val daemon = (daemonResult as? SyfuseResult.Success)?.let { parseDaemonInfo(it.value) }
    val daemonError = (daemonResult as? SyfuseResult.Failure)?.error
    return StatsUiState.Ready(proxy = proxy, daemon = daemon, daemonError = daemonError)
}

/**
 * 解析 `proxies.status`。字段与 Go 侧 `handleProxiesStatus` 一一对应；
 * 缺失即 null，**不补默认值**（`socksAddr` 在未监听时守护进程本就不上报）。
 */
private fun parseProxyStatus(element: JsonElement): ProxyStatus? {
    val obj = element as? JsonObject ?: return null
    return ProxyStatus(
        state = obj.primitiveOrNull("state"),
        running = obj.primitiveOrNull("running")?.toBooleanStrictOrNull(),
        profileId = obj.primitiveOrNull("profileId"),
        selected = obj.primitiveOrNull("selected"),
        nodeCount = obj.primitiveOrNull("nodeCount")?.toIntOrNull(),
        uptimeSec = obj.primitiveOrNull("uptimeSec")?.toLongOrNull(),
        kernelLinked = obj.primitiveOrNull("kernelLinked")?.toBooleanStrictOrNull(),
        socksAddr = obj.primitiveOrNull("socksAddr"),
    )
}

/** 解析 `status`。只取展示需要的三个字段；其余（methods 等）不展示。 */
private fun parseDaemonInfo(element: JsonElement): DaemonInfo? {
    val obj = element as? JsonObject ?: return null
    return DaemonInfo(
        daemonVersion = obj.primitiveOrNull("daemonVersion"),
        protocolVersion = obj.primitiveOrNull("protocolVersion")?.toIntOrNull(),
        uptimeSeconds = obj.primitiveOrNull("uptimeSeconds")?.toLongOrNull(),
    )
}

private fun JsonObject.primitiveOrNull(key: String): String? =
    (this[key] as? JsonPrimitive)?.contentOrNull
