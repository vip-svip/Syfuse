package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.ui.components.EmptyState
import io.github.vipsvip.syfuse.ui.theme.StatusColors
import io.github.vipsvip.syfuse.ui.vm.NodesViewModel

/**
 * 节点页（5.2）。
 *
 * 列表用 LazyColumn 虚拟化：1000+ 节点滚动必须 ≥60fps（7.1）。
 * 延迟用颜色 + 数值双重编码，不单靠颜色（7.4）。
 */
@Composable
fun ProxyScreen(viewModel: NodesViewModel = hiltViewModel()) {
    val state by viewModel.uiState.collectAsState()

    Column(Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = state.query,
                onValueChange = viewModel::onQueryChange,
                label = { Text("搜索节点") },
                singleLine = true,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            OutlinedButton(
                onClick = viewModel::urlTestAll,
                enabled = !state.testing,
            ) { Text(if (state.testing) "测速中" else "测速") }
        }

        state.lastTestMessage?.let { message ->
            Text(
                message,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }

        if (state.visibleNodes.isEmpty()) {
            EmptyState(
                title = if (state.allNodes.isEmpty()) "还没有节点" else "没有匹配的节点",
                description = if (state.allNodes.isEmpty()) {
                    "从订阅链接、二维码或剪贴板导入节点后，它们会出现在这里。"
                } else {
                    "换个关键词试试。搜索会匹配节点名称、服务器地址与协议。"
                },
                actionLabel = if (state.allNodes.isEmpty()) "导入订阅" else null,
                onAction = if (state.allNodes.isEmpty()) ({ }) else null,
            )
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(4.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(
                    start = 16.dp, end = 16.dp, bottom = 16.dp,
                ),
            ) {
                items(state.visibleNodes, key = { it.id }) { node ->
                    NodeRow(node)
                }
            }
        }
    }
}

@Composable
private fun NodeRow(node: ProxyNode) {
    val latencyColor = StatusColors.forLatency(node.latencyMs)
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .semantics {
                contentDescription = buildString {
                    append(node.name)
                    append("，协议 ${node.protocol.name}")
                    append("，延迟 ")
                    append(node.latencyMs?.let { "$it 毫秒" } ?: "未测速")
                }
            },
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(node.name, style = MaterialTheme.typography.bodyLarge)
                Text(
                    "${node.protocol.name} · ${node.server}:${node.port}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Surface(color = latencyColor, shape = CircleShape, modifier = Modifier.width(10.dp).height(10.dp)) {}
            Spacer(Modifier.width(8.dp))
            Text(
                node.latencyMs?.let { "$it ms" } ?: "未测速",
                style = MaterialTheme.typography.labelMedium,
                color = latencyColor,
            )
        }
    }
}
