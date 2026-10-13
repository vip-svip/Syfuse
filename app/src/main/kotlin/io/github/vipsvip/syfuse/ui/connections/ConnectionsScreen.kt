package io.github.vipsvip.syfuse.ui.connections

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
 * 连接 页（M9「数据与统计」其一）。
 *
 * **诚实边界（本仓库第一原则）**：
 *
 *  1. 守护进程的 `connections` 方法尚未实现（见 [DaemonCapability] 的
 *     PENDING 表，计划里程碑 M9），`UdsDataPlane.connections()` 当前只可能
 *     返回空列表。因此本页**不展示任何连接数据** —— 一个空列表会被用户
 *     误读成"当前没有连接"，而那是编造事实。这里如实说明能力状态。
 *  2. 页面下方展示的"代理概况"来自 `proxies.status` 的**真实应答**
 *     （state / selected / nodeCount / socksAddr / uptimeSec / kernelLinked），
 *     守护进程没上报的字段显示"未上报"，不用默认值冒充。
 *  3. 网络只走 [DaemonClient]（由 `EngineModule` 提供的 Singleton），
 *     本文件不自建任何 socket。
 *
 * 关于 [androidx.compose.ui.tooling.preview.Preview]：app 模块的编译类路径
 * 上没有 `ui-tooling-preview`（已用 `:app:dependencies --configuration
 * debugCompileClasspath` 实测确认），因此本文件的 Composable 全部按
 * "纯函数 + 普通数据类入参"组织：内容 composable 不读 Context、不做 IO、不
 * 依赖 Hilt，补上该依赖后即可机械地外包一层 `@Preview`。
 */

/** Hilt 取件点：从 SingletonComponent 取 [DaemonClient]。 */
@EntryPoint
@InstallIn(SingletonComponent::class)
interface DaemonClientEntryPoint {
    fun daemonClient(): DaemonClient
}

/**
 * 连接页入口。导航只需挂这一行：`composable("connections") { ConnectionsEntry() }`。
 *
 * 为什么不用 `@HiltViewModel` + `hiltViewModel()`：app 模块编译类路径上没有
 * hilt-navigation-compose（实测），而 ViewModel 的装配约定在 `ui` 模块。
 * [DaemonClient] 在 SingletonComponent 里由 `EngineModule` 提供，
 * 用 Hilt 文档的 [EntryPointAccessors] 取它是无需改动任何构建文件的方式。
 */
@Composable
fun ConnectionsEntry(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val client = remember(context) {
        val app = context.applicationContext as Application
        EntryPointAccessors.fromApplication(app, DaemonClientEntryPoint::class.java).daemonClient()
    }
    ConnectionsScreen(client = client, modifier = modifier)
}

/** 连接页状态。三态显式分开，不用 null 表达"加载中"或"出错"。 */
private sealed interface ConnectionsUiState {
    data object Loading : ConnectionsUiState

    /** 守护进程不可达 / 返回了结构化错误。原因必须可展示、可重试。 */
    data class Unavailable(val error: SyfuseError) : ConnectionsUiState

    data class Ready(val summary: ProxySummary) : ConnectionsUiState
}

/**
 * `proxies.status` 的展示子集。
 *
 * 每个字段都可为 null —— 那意味着"守护进程没有上报它"。
 * 界面据此显示"未上报"，而不是填一个看起来合理的默认值。
 */
private data class ProxySummary(
    val state: String?,
    val selected: String?,
    val nodeCount: Int?,
    val socksAddr: String?,
    val uptimeSec: Long?,
    val kernelLinked: Boolean?,
)

@Composable
fun ConnectionsScreen(
    client: DaemonClient,
    modifier: Modifier = Modifier,
) {
    var uiState by remember { mutableStateOf<ConnectionsUiState>(ConnectionsUiState.Loading) }

    // 刷新令牌：ErrorState 的重试按钮 +1 即重新发起一次真实读取。
    var reloadToken by remember { mutableStateOf(0) }
    LaunchedEffect(client, reloadToken) {
        uiState = loadProxySummary(client)
    }

    Column(
        modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SectionHeader("连接")
        when (val state = uiState) {
            ConnectionsUiState.Loading -> LoadingState(message = "正在读取守护进程状态…")

            is ConnectionsUiState.Unavailable -> ErrorState(
                userMessage = state.error.userMessage,
                technical = state.error.technical,
                hint = state.error.hint,
                onRetry = { reloadToken++ },
            )

            is ConnectionsUiState.Ready -> {
                ConnectionsPendingNotice()
                ProxySummarySection(state.summary)
            }
        }
    }
}

/**
 * 能力状态声明。
 *
 * 文案由 [DaemonCapability] 驱动：方法实现了就改说"UI 跟进项"，
 * 没实现就报计划里程碑。两条分支都**不展示连接数据** —— 本页还没有
 * 消费该数据的能力，说清楚比留一个空列表诚实。
 */
@Composable
private fun ConnectionsPendingNotice() {
    val implemented = DaemonCapability.isImplemented("connections")
    val milestone = DaemonCapability.pendingMilestone("connections")
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp)
            .semantics { contentDescription = "连接列表不可用，详见卡片说明" },
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(16.dp)) {
            Text("连接列表不可用", style = MaterialTheme.typography.titleMedium)
            Text(
                text = if (implemented) {
                    "守护进程的 connections 方法已实现，但本页尚未接入该数据（UI 跟进项）。" +
                        "在此之前，这里不展示任何连接数据。"
                } else {
                    "守护进程的 connections 方法尚未实现（计划里程碑 ${milestone ?: "未知"}）。" +
                        "在它落地前，本页不展示任何连接数据 —— 空列表会被误读成“当前没有连接”，" +
                        "而那并不是事实。"
                },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** 来自 `proxies.status` 的真实概况。字段缺失显示"未上报"。 */
@Composable
private fun ProxySummarySection(summary: ProxySummary) {
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
            InfoRow("引擎状态", stateLabel(summary.state))
            InfoRow(
                "内核宿主",
                when (summary.kernelLinked) {
                    true -> "已接通"
                    false -> "未接通 —— 配置与选择是真实的，但流量尚未经过代理"
                    null -> "未上报"
                },
            )
            InfoRow("当前选中节点", summary.selected?.takeIf { it.isNotBlank() } ?: "未选择")
            InfoRow("节点数", summary.nodeCount?.toString() ?: "未上报")
            InfoRow(
                "SOCKS5 监听地址",
                summary.socksAddr ?: "未监听（守护进程未启动隧道时不上报该字段）",
            )
            InfoRow("已运行时长", uptimeLabel(summary.uptimeSec))
        }
    }
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

/** 守护进程的原始 state 值 → 可读标签。未收录的值原样显示，不猜。 */
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
 * 运行时长。
 *
 * 注意 0 的语义：守护进程只在真正启动过时才累计 `uptimeSec`，
 * 所以 0 的含义是"尚未开始计时"，不是"运行了 0 秒"。
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

private suspend fun loadProxySummary(client: DaemonClient): ConnectionsUiState =
    when (val result = client.call("proxies.status")) {
        is SyfuseResult.Success -> parseProxySummary(result.value)
            ?.let { ConnectionsUiState.Ready(it) }
            ?: ConnectionsUiState.Unavailable(
                SyfuseError(
                    code = ErrorCode.ENGINE_IPC_FAILED,
                    technical = "proxies.status 返回非预期格式: ${result.value}",
                    userMessage = "守护进程返回了无法解读的代理状态",
                    hint = "App 与模块版本可能不匹配",
                ),
            )

        is SyfuseResult.Failure -> ConnectionsUiState.Unavailable(result.error)
    }

/**
 * 解析 `proxies.status` 应答中本页需要的字段。
 *
 * 与 Go 侧 `handleProxiesStatus` 的字段逐一对应；缺失即 null，**不补默认值**。
 */
private fun parseProxySummary(element: JsonElement): ProxySummary? {
    val obj = element as? JsonObject ?: return null
    return ProxySummary(
        state = obj.primitiveOrNull("state"),
        selected = obj.primitiveOrNull("selected"),
        nodeCount = obj.primitiveOrNull("nodeCount")?.toIntOrNull(),
        socksAddr = obj.primitiveOrNull("socksAddr"),
        uptimeSec = obj.primitiveOrNull("uptimeSec")?.toLongOrNull(),
        kernelLinked = obj.primitiveOrNull("kernelLinked")?.toBooleanStrictOrNull(),
    )
}

private fun JsonObject.primitiveOrNull(key: String): String? =
    (this[key] as? JsonPrimitive)?.contentOrNull
