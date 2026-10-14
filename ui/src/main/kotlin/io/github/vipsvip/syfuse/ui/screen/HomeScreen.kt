package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import io.github.vipsvip.syfuse.domain.engine.EngineState
import io.github.vipsvip.syfuse.domain.model.RoutingMode
import io.github.vipsvip.syfuse.ui.nav.AppRoutes
import io.github.vipsvip.syfuse.ui.theme.StatusColors
import io.github.vipsvip.syfuse.ui.vm.HomeViewModel

/**
 * 首页（5.1）。
 *
 * 信息层级与 Shadowrocket 对齐：顶部大开关 → 全局路由三态 → 当前配置与节点 → 实时速率 → 快捷动作。
 * 过滤拦截计数放在最末尾，确保不干扰主干操作路径（5.1 的显式要求）。
 */
@Composable
fun HomeScreen(onOpen: (String) -> Unit = {}, viewModel: HomeViewModel = hiltViewModel()) {
    val state by viewModel.uiState.collectAsState()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
    ) {
        ConnectionCard(
            stateName = state.status.state.name,
            onToggle = viewModel::toggleConnection,
        )

        Spacer(Modifier.height(16.dp))
        RoutingModeRow(
            current = state.routingMode,
            onSelect = viewModel::setRoutingMode,
        )

        Spacer(Modifier.height(16.dp))
        CurrentSelectionCard(
            profileName = state.currentProfileName,
            nodeName = state.currentNodeName,
        )

        Spacer(Modifier.height(16.dp))
        TrafficCard(
            upPerSec = state.rate.upBytesPerSec,
            downPerSec = state.rate.downBytesPerSec,
            totalUp = state.rate.totalUpBytes,
            totalDown = state.rate.totalDownBytes,
        )

        Spacer(Modifier.height(16.dp))
        QuickActions(
            onUrlTest = viewModel::urlTest,
            onOpen = onOpen,
            message = state.quickActionMessage,
        )

        Spacer(Modifier.height(24.dp))
        Text(
            "过滤规则已拦截 ${state.blockedCount} 次",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun ConnectionCard(stateName: String, onToggle: () -> Unit) {
    val state = runCatching { EngineState.valueOf(stateName) }.getOrDefault(EngineState.STOPPED)
    val color = when (state) {
        EngineState.STOPPED -> StatusColors.idle
        EngineState.STARTING, EngineState.STOPPING -> StatusColors.connecting
        EngineState.RUNNING -> StatusColors.connected
        EngineState.ERROR -> StatusColors.error
    }
    val label = when (state) {
        EngineState.STOPPED -> "未连接"
        EngineState.STARTING -> "连接中"
        EngineState.STOPPING -> "断开中"
        EngineState.RUNNING -> "已连接"
        EngineState.ERROR -> "错误"
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(20.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text("连接", style = MaterialTheme.typography.labelMedium)
                Spacer(Modifier.height(4.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Surface(
                        color = color,
                        shape = CircleShape,
                        modifier = Modifier
                            .size(10.dp)
                            .semantics { contentDescription = "连接状态：$label" },
                    ) {}
                    Spacer(Modifier.size(8.dp))
                    Text(
                        label,
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
            }
            Switch(
                checked = state == EngineState.RUNNING || state == EngineState.STARTING,
                onCheckedChange = { onToggle() },
                modifier = Modifier.semantics { contentDescription = "连接总开关" },
            )
        }
    }
}

/** 全局路由三态。命名必须保留"配置 / 代理 / 直连"（5.1）。 */
@Composable
private fun RoutingModeRow(current: RoutingMode, onSelect: (RoutingMode) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        RoutingMode.entries.forEach { mode ->
            val selected = mode == current
            if (selected) {
                androidx.compose.material3.Button(
                    onClick = { onSelect(mode) },
                    modifier = Modifier.weight(1f),
                ) { Text(mode.displayName) }
            } else {
                OutlinedButton(
                    onClick = { onSelect(mode) },
                    modifier = Modifier.weight(1f),
                ) { Text(mode.displayName) }
            }
        }
    }
}

@Composable
private fun CurrentSelectionCard(profileName: String?, nodeName: String?) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text("当前配置", style = MaterialTheme.typography.labelMedium)
            Text(profileName ?: "未导入配置", style = MaterialTheme.typography.bodyLarge)
            Spacer(Modifier.height(8.dp))
            Text("当前节点", style = MaterialTheme.typography.labelMedium)
            Text(nodeName ?: "未选择节点", style = MaterialTheme.typography.bodyLarge)
        }
    }
}

@Composable
private fun TrafficCard(upPerSec: Double, downPerSec: Double, totalUp: Long, totalDown: Long) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().padding(16.dp)) {
            Column(Modifier.weight(1f)) {
                Text("上行", style = MaterialTheme.typography.labelMedium)
                Text(formatRate(upPerSec), style = MaterialTheme.typography.titleMedium)
                Text("累计 ${formatBytes(totalUp)}", style = MaterialTheme.typography.bodySmall)
            }
            Column(Modifier.weight(1f)) {
                Text("下行", style = MaterialTheme.typography.labelMedium)
                Text(formatRate(downPerSec), style = MaterialTheme.typography.titleMedium)
                Text("累计 ${formatBytes(totalDown)}", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

/**
 * 快捷动作区。测速 / 更新订阅 / 清理 DNS 缓存 / 查看连接（5.1）。
 *
 * 这四个动作原本全是 `onClick = { }`：看着能点，点下去毫无反应也没有任何说明 ——
 * 这正是"像空壳"的观感来源。现在逐个给出真实去处：
 *  - 测速：调用内核 `proxies.urlTest`，结果或失败原因如实回显（不伪造延迟）；
 *  - 更新订阅：跳转代理页，订阅导入实现于彼处；
 *  - 查看连接：跳转连接二级页；
 *  - 清理 DNS 缓存：内核尚未提供 `dns.flush` 接口，**禁用并标注原因**，
 *    而不是保留一个点了没反应的按钮。
 */
@Composable
private fun QuickActions(
    onUrlTest: () -> Unit,
    onOpen: (String) -> Unit,
    message: String?,
) {
    Column {
        Text("快捷动作", style = MaterialTheme.typography.labelMedium)
        Spacer(Modifier.height(8.dp))
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            TextButton(onClick = onUrlTest) {
                Text("测速", style = MaterialTheme.typography.labelMedium)
            }
            TextButton(onClick = { onOpen(AppRoutes.PROXY) }) {
                Text("更新订阅", style = MaterialTheme.typography.labelMedium)
            }
            TextButton(
                onClick = {},
                enabled = false,
                modifier = Modifier.semantics {
                    contentDescription =
                        "清理 DNS 缓存，未实现，内核尚未提供 dns.flush 接口"
                },
            ) {
                Text("清理 DNS 缓存", style = MaterialTheme.typography.labelMedium)
            }
            TextButton(onClick = { onOpen(AppRoutes.CONNECTIONS) }) {
                Text("查看连接", style = MaterialTheme.typography.labelMedium)
            }
        }
        if (message != null) {
            Spacer(Modifier.height(4.dp))
            Text(
                message,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** 速率格式化。单位换算用 1024 进制，与内核上报口径一致。 */
internal fun formatRate(bytesPerSec: Double): String = "${formatBytes(bytesPerSec.toLong())}/s"
