package io.github.vipsvip.syfuse.ui.page.nodes

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.FilterChip
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.unit.dp
import io.github.vipsvip.syfuse.ui.R
import io.github.vipsvip.syfuse.ui.component.UiState
import io.github.vipsvip.syfuse.ui.component.VirtualList

/**
 * 节点页（提示词 5.2 + 5.11）。
 *
 * 层级：顶部策略组横向标签 → 节点虚拟列表。
 *
 * M1 阶段列表为空并渲染空状态 —— 这是**正确**的行为而非缺陷：
 * 没有导入订阅时本就没有节点，渲染空状态并给出引导动作，
 * 正是提示词 7.3 要求的「空状态含引导动作」。
 */
@Composable
fun NodesPage(modifier: Modifier = Modifier) {
    var selectedGroup by remember { mutableStateOf<String?>(null) }

    // M1 无数据源，显式走空状态分支，不伪造示例节点（红线条 R4）
    val state: UiState<List<NodeListItem>> = UiState.Empty(
        title = stringResource(R.string.nodes_empty_title),
        actionLabel = stringResource(R.string.action_import_subscription),
        onAction = { /* M3 接入导入流程 */ },
    )

    Column(modifier = modifier.fillMaxWidth()) {
        GroupTabs(
            groups = emptyList(),
            selected = selectedGroup,
            onSelect = { selectedGroup = it },
        )

        when (state) {
            is UiState.Empty -> EmptyNodesHint(state)
            is UiState.Content -> VirtualList(
                items = state.data,
                key = { it.id },
            ) { node ->
                NodeRow(node)
            }
            else -> Unit
        }
    }
}

/** 策略组横向标签。为空时不渲染区域，避免出现一条无内容的空条。 */
@Composable
private fun GroupTabs(
    groups: List<String>,
    selected: String?,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (groups.isEmpty()) return

    LazyRow(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(groups, key = { it }) { group ->
            FilterChip(
                selected = group == selected,
                onClick = { onSelect(group) },
                label = { Text(group) },
            )
        }
    }
}

/**
 * 空状态引导。
 *
 * 独立于通用 Empty 组件实现，因为节点页的空状态需要额外的操作引导说明
 * （告诉用户「怎么才能有节点」），这是提升首次使用成功率的关键。
 */
@Composable
private fun EmptyNodesHint(state: UiState.Empty, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = state.title ?: stringResource(R.string.state_empty_title),
            style = MaterialTheme.typography.titleMedium,
        )
        Text(
            text = stringResource(R.string.nodes_empty_hint),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        state.actionLabel?.let { label ->
            androidx.compose.material3.Button(onClick = { state.onAction?.invoke() }) {
                Text(label)
            }
        }
    }
}

/** 节点行模型。M3 起由 domain 的 ProxyNode 映射而来。 */
data class NodeListItem(
    val id: String,
    val name: String,
    val protocolBadge: String,
    val latencyMillis: Long?,
    val selected: Boolean,
)

@Composable
private fun NodeRow(node: NodeListItem, modifier: Modifier = Modifier) {
    ListItem(
        modifier = modifier,
        headlineContent = { Text(node.name) },
        supportingContent = { Text(node.protocolBadge, style = MaterialTheme.typography.bodySmall) },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                LatencyBadge(node.latencyMillis)
            }
        },
    )
}

/**
 * 延迟徽标（提示词 5.2：<200ms 绿、<500ms 黄、其余红）。
 *
 * 注意配色采用**语义固定色**而非主题取色：延迟分级的颜色含义是行业约定，
 * 跟随壁纸变化会让用户产生错误判断。同时辅以数值文本，
 * 满足提示词 7.4「不只靠颜色传递信息」。
 */
@Composable
private fun LatencyBadge(latencyMillis: Long?, modifier: Modifier = Modifier) {
    val text = latencyMillis?.let { stringResource(R.string.nodes_latency_ms, it) }
        ?: stringResource(R.string.nodes_never_tested)
    val color = when {
        latencyMillis == null -> MaterialTheme.colorScheme.onSurfaceVariant
        latencyMillis < 200 -> LatencyGreen
        latencyMillis < 500 -> LatencyAmber
        else -> LatencyRed
    }

    Text(text = text, color = color, style = MaterialTheme.typography.labelLarge)
}

private val LatencyGreen = Color(0xFF2E7D32)
private val LatencyAmber = Color(0xFFF9A825)
private val LatencyRed = Color(0xFFC62828)
