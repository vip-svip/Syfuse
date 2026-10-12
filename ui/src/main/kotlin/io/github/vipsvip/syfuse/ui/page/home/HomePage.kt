package io.github.vipsvip.syfuse.ui.page.home

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.vipsvip.syfuse.domain.model.RoutingMode
import io.github.vipsvip.syfuse.ui.R

/**
 * 首页（提示词 5.1）。
 *
 * M1 阶段为静态骨架：展示全部主干控件与信息层级，但不接真实内核状态。
 *
 * 四态颜色语义（未连接 / 连接中 / 已连接 / 错误）在此定义，
 * 因为它是提示词 5.1 明确要求的「错误态要能点进看原因」。
 */
@Composable
fun HomePage(modifier: Modifier = Modifier) {
    // M2 起改由 ViewModel 提供；M1 用本地状态保证骨架可交互可截图
    var connectionState by remember { mutableStateOf(ConnectionState.DISCONNECTED) }
    var routingMode by remember { mutableStateOf(RoutingMode.RULE) }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ConnectionSwitch(
            state = connectionState,
            onToggle = {
                connectionState = if (connectionState == ConnectionState.CONNECTED) {
                    ConnectionState.DISCONNECTED
                } else {
                    // 骨架阶段不进「连接中」，避免伪装成真有内核在握手（红线条 R4）
                    ConnectionState.DISCONNECTED
                }
            },
        )

        RoutingModeSelector(
            selected = routingMode,
            onSelect = { routingMode = it },
        )

        ProfileNodeCard(
            profileName = stringResource(R.string.no_profile),
            nodeName = stringResource(R.string.no_node),
        )

        TrafficCard(upBytesPerSec = 0, downBytesPerSec = 0, totalUp = 0, totalDown = 0)

        QuickActionsRow()

        FilterHitsCard(hits = 0)
    }
}

/**
 * 连接状态。
 *
 * 四种状态的配色是提示词 5.1 的硬性要求，且必须语义明确：
 * 绿色仅用于「已连接」，红色仅用于「错误」，中间态用中性色，
 * 避免用户误判（这也是提示词 7.4 对「不只靠颜色传递信息」的呼应，
 * 故每态同时有文字标签）。
 */
enum class ConnectionState { DISCONNECTED, CONNECTING, CONNECTED, ERROR }

@Composable
private fun ConnectionSwitch(
    state: ConnectionState,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val (color, labelRes) = when (state) {
        ConnectionState.DISCONNECTED -> MaterialTheme.colorScheme.surfaceVariant to R.string.state_disconnected
        ConnectionState.CONNECTING -> MaterialTheme.colorScheme.tertiary to R.string.state_connecting
        ConnectionState.CONNECTED -> ConnectedGreen to R.string.state_connected
        ConnectionState.ERROR -> MaterialTheme.colorScheme.error to R.string.state_error
    }
    val label = stringResource(labelRes)

    Box(
        modifier = modifier.fillMaxWidth(),
        contentAlignment = Alignment.Center,
    ) {
        Surface(
            modifier = Modifier
                .size(180.dp)
                // 触控目标远超 48dp，满足提示词 7.4 的可访问性下限
                .semantics { contentDescription = label }
                .clickable(onClick = onToggle),
            shape = CircleShape,
            color = color,
            tonalElevation = 3.dp,
        ) {
            Box(contentAlignment = Alignment.Center) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(
                        text = label,
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
        }
    }
}

/**
 * 全局路由三态。
 *
 * 命名「配置 / 代理 / 直连」直接取自 [RoutingMode.displayName]，
 * 不做二次翻译，以保证与 Shadowrocket 概念同名（提示词 3.2 P0 规则）。
 */
@Composable
private fun RoutingModeSelector(
    selected: RoutingMode,
    onSelect: (RoutingMode) -> Unit,
    modifier: Modifier = Modifier,
) {
    val modes = RoutingMode.entries
    Column(modifier = modifier.fillMaxWidth()) {
        Text(
            text = stringResource(R.string.home_routing_mode),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        SingleChoiceSegmentedButtonRow(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 8.dp),
        ) {
            modes.forEachIndexed { index, mode ->
                SegmentedButton(
                    selected = mode == selected,
                    onClick = { onSelect(mode) },
                    shape = SegmentedButtonDefaults.itemShape(index = index, count = modes.size),
                ) {
                    Text(mode.displayName)
                }
            }
        }
    }
}

@Composable
private fun ProfileNodeCard(profileName: String, nodeName: String, modifier: Modifier = Modifier) {
    Card(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Column {
                Text(
                    text = stringResource(R.string.home_current_profile),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(text = profileName, style = MaterialTheme.typography.titleMedium)
            }
            Column(horizontalAlignment = Alignment.End) {
                Text(
                    text = stringResource(R.string.home_current_node),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(text = nodeName, style = MaterialTheme.typography.titleMedium)
            }
        }
    }
}

@Composable
private fun TrafficCard(
    upBytesPerSec: Long,
    downBytesPerSec: Long,
    totalUp: Long,
    totalDown: Long,
    modifier: Modifier = Modifier,
) {
    Card(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            horizontalArrangement = Arrangement.SpaceEvenly,
        ) {
            TrafficColumn(
                label = stringResource(R.string.home_upload),
                rate = formatRate(upBytesPerSec),
                total = formatBytes(totalUp),
            )
            TrafficColumn(
                label = stringResource(R.string.home_download),
                rate = formatRate(downBytesPerSec),
                total = formatBytes(totalDown),
            )
        }
    }
}

@Composable
private fun TrafficColumn(label: String, rate: String, total: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(text = rate, style = MaterialTheme.typography.titleMedium)
        Text(
            text = total,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun QuickActionsRow(modifier: Modifier = Modifier) {
    Column(modifier = modifier.fillMaxWidth()) {
        Text(
            text = stringResource(R.string.home_quick_actions),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        LazyRow(
            modifier = Modifier.padding(top = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            // 四个快捷动作（提示词 5.1）。M1 无行为，按钮只有视觉与语义
            val actions = listOf(
                R.string.action_speed_test,
                R.string.action_update_subscription,
                R.string.action_flush_dns,
                R.string.action_view_connections,
            )
            itemsIndexed(actions) { _, labelRes ->
                FilledTonalButton(onClick = { /* M2/M3 接入 */ }) {
                    Text(stringResource(labelRes))
                }
            }
        }
    }
}

/**
 * 过滤拦截计数（提示词 5.1 要求「首页可放一处轻量统计，位置不得干扰主干」）。
 *
 * 因此放在页面最底部，且不占用大开关附近的视觉焦点。
 */
@Composable
private fun FilterHitsCard(hits: Long, modifier: Modifier = Modifier) {
    Card(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = stringResource(R.string.home_filter_hits),
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(text = hits.toString(), style = MaterialTheme.typography.titleMedium)
        }
    }
}

/**
 * 「已连接」用的绿色。
 *
 * 单列常量的理由：这是语义色而非主题色，不能随动态取色变化，
 * 否则在某些壁纸配色下会与「错误红」难以区分。
 */
private val ConnectedGreen = Color(0xFF2E7D32)

/** 速率格式化。单位用二进制（KiB/MiB），与系统流量统计口径一致。 */
internal fun formatRate(bytesPerSec: Long): String = "${formatBytes(bytesPerSec)}/s"

internal fun formatBytes(bytes: Long): String {
    if (bytes < 1024) return "$bytes B"
    val units = listOf("KiB", "MiB", "GiB", "TiB")
    var value = bytes.toDouble() / 1024
    var unitIndex = 0
    while (value >= 1024 && unitIndex < units.lastIndex) {
        value /= 1024
        unitIndex++
    }
    return String.format("%.1f %s", value, units[unitIndex])
}
