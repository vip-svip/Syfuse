package io.github.vipsvip.syfuse.ui.screen

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
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.ui.components.ErrorState
import io.github.vipsvip.syfuse.ui.components.LoadingState
import io.github.vipsvip.syfuse.ui.components.SectionHeader
import io.github.vipsvip.syfuse.ui.vm.DaemonInfo
import io.github.vipsvip.syfuse.ui.vm.ProxyStatus
import io.github.vipsvip.syfuse.ui.vm.StatsUiState
import io.github.vipsvip.syfuse.ui.vm.StatsViewModel
import io.github.vipsvip.syfuse.ui.vm.TrafficTotals

/**
 * 统计 页（M9「数据与统计」其三）。
 *
 * **诚实边界（本仓库第一原则）**：
 *
 *  1. 页面只展示**守护进程真实应答**里的字段：`proxies.status`（引擎状态 /
 *     选中节点 / 节点数 / 监听地址 / 运行时长 / 内核接通情况）、`status`
 *     （守护进程版本 / 协议版本 / 进程运行时长）、`traffic`（累计上/下行字节
 *     与连接计数）、`connections`（活动连接数）。没上报的字段显示"未上报"。
 *  2. 累计流量与连接计数分别独立读取：任一路失败只让那一段显示"未上报"，
 *     不会把它显示成 0 —— 0 会被用户误读成"没有流量 / 没有连接"。
 *  3. 连接计数按 `endTime` 判活动（Go 零值 time 序列化为
 *     "0001-01-01T00:00:00Z"），不是拿 records 长度充数。
 *  4. 网络只走 `DaemonClient`（`EngineModule` 提供的 Singleton），
 *     本文件不自建任何 socket。
 *
 * **本页原先写在 `app` 模块且无法被导航到达**（依赖方向 `app → ui`，
 * 而导航树在 `ui`），属于"功能不生效"。现已迁到 `ui` 并在
 * `SyfuseApp.kt` 挂上 `composable("stats")`，同时改用 `ui` 模块既有的
 * `@HiltViewModel` + `hiltViewModel()` 装配约定。
 */

/** 统计页入口。已挂在导航路由 `"stats"` 上（`SyfuseApp.kt`）。 */
@Composable
fun StatsEntry(modifier: Modifier = Modifier) {
    val vm: StatsViewModel = hiltViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    StatsScreen(
        state = state,
        onRetry = { vm.refresh() },
        modifier = modifier,
    )
}

/**
 * 纯渲染：输入 state、输出 UI，不做 IO、不读 Context。
 * 这正是 `@Preview` 与 JVM 单测都能直接驱动的形态。
 */
@Composable
fun StatsScreen(
    state: StatsUiState,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SectionHeader("统计")
        when (state) {
            StatsUiState.Loading -> LoadingState(message = "正在读取守护进程数据…")

            is StatsUiState.Unavailable -> ErrorState(
                userMessage = state.error.userMessage,
                technical = state.error.technical,
                hint = state.error.hint,
                onRetry = onRetry,
            )

            is StatsUiState.Ready -> {
                ProxyStatusSection(state.proxy)
                TrafficSection(state.traffic, state.connectionCount)
                DaemonSection(state.daemon, state.daemonError)
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
                "用户已请求启动",
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
 * 流量与连接计数（`traffic` + `connections` 的真实数据）。
 *
 * 两段数据各自可能为 null，含义是"这一轮没读到"，分别显示"未上报" ——
 * 与"读到了、值就是 0"严格区分开。
 */
@Composable
private fun TrafficSection(traffic: TrafficTotals?, connectionCount: Int?) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                "流量与连接（来自 traffic / connections）",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            InfoRow("累计上行（客户端→上游）", traffic?.let { formatBytes(it.totalBytesIn) } ?: "未上报")
            InfoRow("累计下行（上游→客户端）", traffic?.let { formatBytes(it.totalBytesOut) } ?: "未上报")
            InfoRow("活动连接数", connectionCount?.toString() ?: "未上报")
            InfoRow(
                "连接总数（含已关闭）",
                traffic?.totalConnections?.toString() ?: "未上报",
            )
            if (traffic == null && connectionCount == null) {
                Text(
                    "守护进程这两项都没读到 —— 可能是引擎尚未启动（此时统计本为零），" +
                        "也可能是这一轮 IPC 失败。显示“未上报”而不是 0，是为了不让" +
                        "“读不到”被当成“没有流量”。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }
        }
    }
}

/**
 * 字节数的人读格式。
 *
 * 用 1024 进制并对齐到一位小数；0 显示成 "0 B"（这里是**读到的真值**，
 * 与"未上报"由调用点区分）。
 */
internal fun formatBytes(bytes: Long): String {
    if (bytes < 1024) return "$bytes B"
    val units = listOf("KB", "MB", "GB", "TB")
    var value = bytes.toDouble() / 1024
    var unitIndex = 0
    while (value >= 1024 && unitIndex < units.lastIndex) {
        value /= 1024
        unitIndex++
    }
    return "${(value * 10).toLong() / 10.0} ${units[unitIndex]}"
}

/**
 * 一行"标签 + 值"。与 ui 模块 SettingRow 的三段式保持同一视觉语言。
 *
 * `internal` 而非 `private`：连接页（`ConnectionsScreen.kt`）也用它，
 * 两个文件同属 `io.github.vipsvip.syfuse.ui.screen` 包。
 */
@Composable
internal fun InfoRow(label: String, value: String) {
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
internal fun stateLabel(state: String?): String = when (state) {
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
internal fun uptimeLabel(seconds: Long?): String = when {
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
