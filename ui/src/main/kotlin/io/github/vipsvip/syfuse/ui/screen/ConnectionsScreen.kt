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
import io.github.vipsvip.syfuse.ui.vm.ConnRow
import io.github.vipsvip.syfuse.ui.vm.ConnectionsUiState
import io.github.vipsvip.syfuse.ui.vm.ConnectionsViewModel
import io.github.vipsvip.syfuse.ui.vm.ProxySummary

/**
 * 连接 页（M9「数据与统计」其一）。
 *
 * **诚实边界（本仓库第一原则）**：
 *
 *  1. 连接列表来自守护进程 `connections` 的**真实应答**（Go 侧
 *     `internal/conn.Tracker` 记录每条真实连接）。引擎尚未启动时守护进程
 *     返回空 records —— 那时显示"暂无连接"是**事实**，不是猜测。
 *     `endTime` 为零值（"0001-01-01T00:00:00Z"）表示连接仍活动。
 *  2. 页面下方展示的"代理概况"来自 `proxies.status` 的**真实应答**
 *     （state / selected / nodeCount / socksAddr / uptimeSec / kernelLinked），
 *     守护进程没上报的字段显示"未上报"，不用默认值冒充。
 *  3. 网络只走 `DaemonClient`（由 `EngineModule` 提供的 Singleton），
 *     本文件不自建任何 socket。
 *
 * **本页原先写在 `app` 模块且无法被导航到达**（依赖方向 `app → ui`，
 * 而导航树在 `ui`），属于"功能不生效"。现已迁到 `ui` 并在
 * `SyfuseApp.kt` 挂上 `composable("connections")`。
 */

/** 连接页入口。已挂在导航路由 `"connections"` 上（`SyfuseApp.kt`）。 */
@Composable
fun ConnectionsEntry(modifier: Modifier = Modifier) {
    val vm: ConnectionsViewModel = hiltViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    ConnectionsScreen(
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
fun ConnectionsScreen(
    state: ConnectionsUiState,
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
        SectionHeader("连接")
        when (state) {
            ConnectionsUiState.Loading -> LoadingState(message = "正在读取守护进程状态…")

            is ConnectionsUiState.Unavailable -> ErrorState(
                userMessage = state.error.userMessage,
                technical = state.error.technical,
                hint = state.error.hint,
                onRetry = onRetry,
            )

            is ConnectionsUiState.Ready -> {
                ConnectionListSection(state.connections, state.connectionsError)
                ProxySummarySection(state.summary)
            }
        }
    }
}

/**
 * 连接列表。
 *
 * 三种情况严格分开，绝不混为一谈：
 *  - [connectionsError] 非空 → 这一轮**读不到**，如实报错并可重试；
 *  - 列表为空且无错   → 守护进程如实回了 0 条，显示"暂无连接"；
 *  - 有数据           → 逐条展示真实字段。
 */
@Composable
private fun ConnectionListSection(connections: List<ConnRow>, connectionsError: SyfuseError?) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp)
            .semantics {
                contentDescription = if (connectionsError != null) {
                    "连接列表读取失败"
                } else {
                    "连接列表，共 ${connections.size} 条"
                }
            },
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                "连接（来自 connections）",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            when {
                connectionsError != null -> Text(
                    "这一轮没能读到连接列表：${connectionsError.userMessage}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )

                connections.isEmpty() -> Text(
                    "守护进程报告当前没有连接。引擎未启动时这就是真实的零值。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )

                else -> connections.forEach { ConnectionRow(it) }
            }
        }
    }
}

/** 单条连接。只渲染守护进程真的给了的字段。 */
@Composable
private fun ConnectionRow(conn: ConnRow) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .semantics {
                contentDescription = "连接 ${conn.id}：${conn.dstHost ?: "目标未上报"}，" +
                    if (conn.active) "活动" else "已关闭"
            },
    ) {
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                // 目标主机是用户最关心的字段；没上报就如实说，不用 IP 或端口顶上。
                conn.dstHost?.takeIf { it.isNotBlank() } ?: "目标未上报",
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                if (conn.active) "活动" else "已关闭",
                style = MaterialTheme.typography.bodySmall,
                color = if (conn.active) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant
                },
            )
        }
        val details = buildList {
            add("#${conn.id}")
            conn.network.takeIf { it.isNotBlank() }?.let { add(it) }
            conn.type.takeIf { it.isNotBlank() }?.let { add(if (it == "proxy") "经代理" else it) }
            add("↑${formatBytes(conn.upBytes)} ↓${formatBytes(conn.downBytes)}")
            // 这几项内核当前不上报 —— 显示"未上报"而不是留空或编造。
            add("规则 ${conn.ruleText ?: "未上报"}")
            add("链路 ${conn.chain ?: "未上报"}")
        }
        Text(
            details.joinToString(" · "),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
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
