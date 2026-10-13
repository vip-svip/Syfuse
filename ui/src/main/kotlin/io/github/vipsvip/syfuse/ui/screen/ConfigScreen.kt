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
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import io.github.vipsvip.syfuse.core.rule.Decision
import io.github.vipsvip.syfuse.core.rule.Priority
import io.github.vipsvip.syfuse.core.rule.RuleDiagnostic
import io.github.vipsvip.syfuse.ui.components.EmptyState
import io.github.vipsvip.syfuse.ui.vm.RulesViewModel

/**
 * 规则页（5.4）。
 *
 * 分段控件包含：分流规则 / 规则集 / 规则体检 / 规则测试。
 * 注意：过滤规则（差异增强层，6.1）是这里的**一个分段**，而不是并列的新 Tab——
 * 这满足 6.1 的 UI 要求与第 4 部分的去重裁决。
 */
@Composable
fun ConfigScreen(viewModel: RulesViewModel = hiltViewModel()) {
    val state by viewModel.uiState.collectAsState()
    var segment by remember { mutableIntStateOf(0) }
    val segments = listOf("分流规则", "规则集", "规则体检", "规则测试")

    Column(Modifier.fillMaxSize()) {
        SingleChoiceSegmentedButtonRow(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        ) {
            segments.forEachIndexed { index, label ->
                SegmentedButton(
                    selected = segment == index,
                    onClick = { segment = index },
                    shape = SegmentedButtonDefaults.itemShape(index = index, count = segments.size),
                ) { Text(label, style = MaterialTheme.typography.labelSmall) }
            }
        }

        when (segment) {
            0 -> RuleSummary(state.ruleCount, state.indexedCount, state.linearCount)
            1 -> RuleSetsPlaceholder()
            2 -> RuleHealth(state.errors, state.warnings)
            3 -> RuleTester(
                input = state.testInput,
                asUrl = state.testAsUrl,
                onInputChange = viewModel::onTestInputChange,
                onAsUrlChange = viewModel::setTestAsUrl,
                onRun = viewModel::runTest,
                result = state.testResult,
                error = state.testError,
            )
        }
    }
}

@Composable
private fun RuleSummary(total: Int, indexed: Int, linear: Int) {
    if (total == 0) {
        EmptyState(
            title = "还没有规则",
            description = "在设置 → 规则 → 规则源管理里添加本地规则文件或远程规则集。",
            actionLabel = "去设置",
            onAction = { },
        )
        return
    }
    Column(Modifier.padding(16.dp)) {
        Text("规则统计", style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        StatLine("规则总数", total.toString())
        StatLine("索引化（域名 Trie / 网段 Trie）", indexed.toString())
        StatLine("线性扫描（正则 / 通配）", linear.toString())
        Spacer(Modifier.height(12.dp))
        Text(
            "线性扫描的规则越多，单次匹配越慢。把可表达的规则改写成 ||域名^ 形式可以显著提升性能。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun StatLine(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, style = MaterialTheme.typography.bodyMedium)
        Text(value, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun RuleSetsPlaceholder() {
    EmptyState(
        title = "没有规则集",
        description = "规则集支持 domain / ipcidr / classical / mrs 四种格式，可设置更新间隔与哈希校验。",
        actionLabel = "添加规则集",
        onAction = { },
    )
}

/** 规则体检：语法错误必须给出行号与原因（5.4）。 */
@Composable
private fun RuleHealth(errors: List<RuleDiagnostic>, warnings: List<RuleDiagnostic>) {
    if (errors.isEmpty() && warnings.isEmpty()) {
        EmptyState(
            title = "规则体检通过",
            description = "未发现语法错误或可疑写法。",
        )
        return
    }
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(errors) { diagnostic -> DiagnosticCard(diagnostic, isError = true) }
        items(warnings) { diagnostic -> DiagnosticCard(diagnostic, isError = false) }
    }
}

@Composable
private fun DiagnosticCard(diagnostic: RuleDiagnostic, isError: Boolean) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = if (isError) {
                MaterialTheme.colorScheme.errorContainer
            } else {
                MaterialTheme.colorScheme.surfaceVariant
            },
        ),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(
                "第 ${diagnostic.lineNo} 行 · ${if (isError) "错误" else "警告"}",
                style = MaterialTheme.typography.labelMedium,
            )
            Text(diagnostic.message, style = MaterialTheme.typography.bodyMedium)
            if (diagnostic.raw.isNotBlank()) {
                Spacer(Modifier.height(4.dp))
                Text(
                    diagnostic.raw,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** 规则测试：输入域名或 URL，输出命中规则与完整匹配链路（5.4）。 */
@Composable
private fun RuleTester(
    input: String,
    asUrl: Boolean,
    onInputChange: (String) -> Unit,
    onAsUrlChange: (Boolean) -> Unit,
    onRun: () -> Unit,
    result: io.github.vipsvip.syfuse.core.rule.MatchResult?,
    error: String?,
) {
    Column(Modifier.fillMaxSize().padding(16.dp)) {
        OutlinedTextField(
            value = input,
            onValueChange = onInputChange,
            label = { Text("输入域名或 URL") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Switch(checked = asUrl, onCheckedChange = onAsUrlChange)
            Spacer(Modifier.width(8.dp))
            Text("按完整 URL 匹配", style = MaterialTheme.typography.bodyMedium)
        }
        Spacer(Modifier.height(8.dp))
        Button(onClick = onRun, modifier = Modifier.fillMaxWidth()) { Text("测试") }

        if (error != null) {
            Spacer(Modifier.height(12.dp))
            Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
        }

        if (result != null) {
            Spacer(Modifier.height(16.dp))
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    Text(
                        "最终动作：${decisionLabel(result.decision)}",
                        style = MaterialTheme.typography.titleMedium,
                    )
                    Text(
                        "优先级：${priorityLabel(result.priority)}",
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    result.matchedRule?.let { rule ->
                        Spacer(Modifier.height(8.dp))
                        Text("命中规则（第 ${rule.lineNo} 行）", style = MaterialTheme.typography.labelMedium)
                        Text(rule.raw, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }

            Spacer(Modifier.height(12.dp))
            Text("匹配链路（${result.chain.size} 条候选）", style = MaterialTheme.typography.labelMedium)
            Spacer(Modifier.height(4.dp))
            LazyColumn(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                items(result.chain) { entry ->
                    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
                        Text(
                            if (entry.applied) "✓" else "·",
                            style = MaterialTheme.typography.bodySmall,
                            color = if (entry.applied) {
                                MaterialTheme.colorScheme.primary
                            } else {
                                MaterialTheme.colorScheme.onSurfaceVariant
                            },
                        )
                        Spacer(Modifier.width(8.dp))
                        Column(Modifier.weight(1f)) {
                            Text(
                                "第 ${entry.lineNo} 行 ${entry.raw}",
                                style = MaterialTheme.typography.bodySmall,
                            )
                            entry.skippedReason?.let {
                                Text(
                                    it,
                                    style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun decisionLabel(decision: Decision): String = when (decision) {
    Decision.BLOCK -> "拦截"
    Decision.ALLOW -> "放行"
    Decision.REWRITE -> "重写"
    Decision.NONE -> "未命中（默认放行）"
}

private fun priorityLabel(priority: Priority): String = when (priority) {
    Priority.IMPORTANT_ALLOW -> "重要放行"
    Priority.IMPORTANT_BLOCK -> "重要拦截"
    Priority.NORMAL_ALLOW -> "普通放行"
    Priority.NORMAL_BLOCK -> "普通拦截"
    Priority.NONE -> "无"
}
