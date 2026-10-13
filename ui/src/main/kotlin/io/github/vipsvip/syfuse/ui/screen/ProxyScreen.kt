package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
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
import androidx.core.content.ContextCompat
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

    // 导入弹窗与剪贴板。
    val context = LocalContext.current
    var urlDraft by remember { mutableStateOf("") }
    var textDraft by remember { mutableStateOf("") }
    val clipboard = remember {
        ContextCompat.getSystemService(context, android.content.ClipboardManager::class.java)
    }

    Box(Modifier.fillMaxSize()) {
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
                OutlinedButton(onClick = viewModel::urlTestAll, enabled = !state.testing) {
                    Text(if (state.testing) "测速中" else "测速")
                }
                Spacer(Modifier.width(8.dp))
                // 导入是节点页的主入口：没有节点时它是唯一出路，有节点时它是刷新手段。
                Button(onClick = viewModel::showImportDialog, enabled = !state.importing) {
                    Text(if (state.importing) "导入中" else "导入")
                }
            }

            state.importMessage?.let { message ->
                Card(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.secondaryContainer,
                    ),
                ) {
                    Text(
                        message,
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.padding(12.dp),
                    )
                }
            }

            if (state.visibleNodes.isEmpty()) {
                EmptyState(
                    title = if (state.allNodes.isEmpty()) "还没有节点" else "没有匹配的节点",
                    description = if (state.allNodes.isEmpty()) {
                        "导入订阅链接，或粘贴一段订阅内容。支持分享链接列表、Base64 包裹与 Clash 配置。"
                    } else {
                        "换个关键词试试。搜索会匹配节点名称、服务器地址与协议。"
                    },
                    actionLabel = if (state.allNodes.isEmpty()) "导入订阅" else null,
                    onAction = if (state.allNodes.isEmpty()) viewModel::showImportDialog else null,
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

        if (state.importing) {
            // 线性进度条：导入是网络操作，耗时不定，必须有可见的等待反馈。
            LinearProgressIndicator(Modifier.fillMaxWidth().align(Alignment.TopCenter))
        }
    }

    if (state.importDialogVisible) {
        AlertDialog(
            onDismissRequest = viewModel::dismissImportDialog,
            title = { Text("导入订阅") },
            text = {
                Column {
                    Text(
                        "支持三种形态：分享链接列表、Base64 包裹的内容、Clash 配置。会自动识别，无需手动选择。",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedTextField(
                        value = urlDraft,
                        onValueChange = { urlDraft = it },
                        label = { Text("订阅链接") },
                        placeholder = { Text("https://") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Spacer(Modifier.height(8.dp))
                    Text(
                        "或粘贴内容（没有链接时用）",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    OutlinedTextField(
                        value = textDraft,
                        onValueChange = { textDraft = it },
                        label = { Text("订阅内容") },
                        minLines = 3,
                        maxLines = 6,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            },
            confirmButton = {
                TextButton(
                    enabled = !state.importing,
                    onClick = {
                        // 两个入口的优先级：填了链接就按链接走（能拿到流量与到期信息），
                        // 否则按粘贴内容走。
                        if (urlDraft.isNotBlank()) {
                            viewModel.importFromUrl(urlDraft)
                        } else {
                            viewModel.importFromText(textDraft)
                        }
                        viewModel.dismissImportDialog()
                    },
                ) { Text("导入") }
            },
            dismissButton = {
                Row {
                    // 从剪贴板粘贴：订阅链接最常见的来源就是复制来的地址。
                    TextButton(
                        onClick = {
                            val clip = clipboard?.primaryClip
                            val pasted = clip?.getItemAt(0)?.text?.toString().orEmpty()
                            if (pasted.isNotBlank()) {
                                if (pasted.contains("://")) urlDraft = pasted else textDraft = pasted
                            }
                        },
                    ) { Text("粘贴") }
                    TextButton(onClick = viewModel::dismissImportDialog) { Text("取消") }
                }
            },
        )
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
