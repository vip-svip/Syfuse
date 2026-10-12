package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import io.github.vipsvip.syfuse.domain.settings.RiskLevel
import io.github.vipsvip.syfuse.domain.settings.SettingSpec
import io.github.vipsvip.syfuse.ui.components.EmptyState
import io.github.vipsvip.syfuse.ui.components.LoadingState
import io.github.vipsvip.syfuse.ui.components.RiskBadge
import io.github.vipsvip.syfuse.ui.components.SectionHeader
import io.github.vipsvip.syfuse.ui.components.SettingRow
import io.github.vipsvip.syfuse.ui.vm.SettingsViewModel

/**
 * 设置页（5.10）。
 *
 * 结构：设置搜索 → 分组列表 → 每项三段式（标题 + 副标题 + 控件）→ 风险三色标记。
 * 高级项由「专家模式」统一揭示；搜索范围包含被隐藏的项。
 */
@Composable
fun SettingsScreen(viewModel: SettingsViewModel = hiltViewModel()) {
    val state by viewModel.uiState.collectAsState()

    if (state.loading) {
        LoadingState(message = "正在读取设置")
        return
    }

    val error = state.error
    if (error != null) {
        EmptyState(
            title = "设置读取失败",
            description = error,
            actionLabel = "重试",
            onAction = { },
        )
        return
    }

    Column(Modifier.fillMaxSize()) {
        OutlinedTextField(
            value = state.query,
            onValueChange = viewModel::onQueryChange,
            label = { Text("搜索设置") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth().padding(16.dp),
        )

        if (state.query.isNotBlank()) {
            if (state.searchResults.isEmpty()) {
                EmptyState(
                    title = "没有匹配的设置项",
                    description = "试试更短的关键词，例如「DNS」「MTU」「解密」。",
                )
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp),
                ) {
                    item { SectionHeader("搜索结果（${state.searchResults.size} 项）") }
                    items(state.searchResults, key = { it.key }) { spec ->
                        SettingRow(
                            spec = spec,
                            value = state.settings.get(spec.key),
                            onToggle = { viewModel.toggle(spec) },
                            onClick = { viewModel.requestAction(spec) },
                        )
                    }
                }
            }
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(4.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp),
            ) {
                item {
                    OutlinedButton(
                        onClick = viewModel::resetToRecommended,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    ) { Text("恢复全部推荐值") }
                }
                state.visibleGroups().forEach { (group, specs) ->
                    item(key = "header_${group.name}") { SectionHeader(group.displayName) }
                    items(specs, key = { it.key }) { spec ->
                        SettingRow(
                            spec = spec,
                            value = state.settings.get(spec.key),
                            onToggle = { viewModel.toggle(spec) },
                            onClick = { viewModel.requestAction(spec) },
                        )
                    }
                }
            }
        }
    }

    state.pendingAction?.let { spec ->
        ConfirmDialog(
            spec = spec,
            onConfirm = viewModel::confirmPending,
            onDismiss = viewModel::dismissAction,
        )
    }
}

/**
 * 危险操作二次确认（R6 / 7.3）。
 * 对话框必须说明影响范围，而不是只问"确定吗"。
 */
@Composable
private fun ConfirmDialog(spec: SettingSpec, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(spec.title) },
        text = {
            Column {
                RiskBadge(spec.risk)
                Text(spec.subtitle, modifier = Modifier.padding(top = 8.dp))
                if (spec.risk == RiskLevel.DANGEROUS) {
                    Text(
                        "这是危险操作，可能修改系统状态或降低安全性。所有改动都可一键完全还原。",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(if (spec.risk == RiskLevel.DANGEROUS) "我已了解，继续" else "确定")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}
