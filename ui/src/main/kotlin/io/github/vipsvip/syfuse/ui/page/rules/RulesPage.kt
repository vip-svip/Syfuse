package io.github.vipsvip.syfuse.ui.page.rules

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import io.github.vipsvip.syfuse.ui.R
import io.github.vipsvip.syfuse.ui.component.UiState
import io.github.vipsvip.syfuse.ui.component.VirtualList

/**
 * 规则页（提示词 5.4 + 5.11 + 6.1）。
 *
 * 分段控件：分流规则 / 规则集 / 规则体检 / 规则测试 / **过滤规则**。
 *
 * 「过滤规则」是差异增强层（6.1）的入口，但它作为本页的**第五个分段**存在，
 * 而不是新建并列 Tab —— 这是提示词 6.1 的硬性要求：
 * 「UI 要求：作为「规则」页的一个分段，不要新建并列 Tab」。
 * 该约束的存在是为了防止主干功能多绕一步（提示词 4 去重裁决第 3 条）。
 */
@Composable
fun RulesPage(modifier: Modifier = Modifier) {
    var segment by remember { mutableStateOf(RuleSegment.ROUTING) }

    Column(modifier = modifier.fillMaxWidth()) {
        SegmentSelector(
            segments = RuleSegment.entries,
            selected = segment,
            onSelect = { segment = it },
        )

        val state: UiState<List<RuleListItem>> = UiState.Empty(
            title = stringResource(segment.emptyTitleRes),
            actionLabel = stringResource(R.string.rules_add_source),
            onAction = { /* M4 接入规则源管理 */ },
        )

        when (state) {
            is UiState.Content -> VirtualList(items = state.data, key = { it.id }) { rule ->
                RuleRow(rule)
            }
            is UiState.Empty -> EmptyHint(state)
            else -> Unit
        }
    }
}

/**
 * 规则页分段。
 *
 * [emptyTitleRes] 与 [descriptionRes] 随分段变化，因为五个分段的空状态引导完全不同：
 * 「规则集为空」要引导去添加规则源，「规则体检为空」则应说明「暂无语法问题」。
 */
enum class RuleSegment(val titleRes: Int, val emptyTitleRes: Int, val descriptionRes: Int) {
    ROUTING(R.string.rules_segment_routing, R.string.rules_routing_empty, R.string.rules_routing_desc),
    RULE_SETS(R.string.rules_segment_rulesets, R.string.rules_rulesets_empty, R.string.rules_rulesets_desc),
    LINT(R.string.rules_segment_lint, R.string.rules_lint_empty, R.string.rules_lint_desc),
    TEST(R.string.rules_segment_test, R.string.rules_test_empty, R.string.rules_test_desc),
    FILTER(R.string.rules_segment_filter, R.string.rules_filter_empty, R.string.rules_filter_desc),
}

@Composable
private fun SegmentSelector(
    segments: List<RuleSegment>,
    selected: RuleSegment,
    onSelect: (RuleSegment) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyRow(modifier = modifier.padding(horizontal = 16.dp)) {
        items(segments, key = { it.name }) { item ->
            androidx.compose.material3.FilterChip(
                modifier = Modifier.padding(end = 8.dp),
                selected = item == selected,
                onClick = { onSelect(item) },
                label = { Text(stringResource(item.titleRes)) },
            )
        }
    }
}

@Composable
private fun EmptyHint(state: UiState.Empty, modifier: Modifier = Modifier) {
    Column(modifier = modifier.padding(32.dp)) {
        Text(
            text = state.title ?: stringResource(R.string.state_empty_title),
            style = MaterialTheme.typography.titleMedium,
        )
        state.actionLabel?.let {
            androidx.compose.material3.TextButton(onClick = { state.onAction?.invoke() }) {
                Text(it)
            }
        }
    }
}

/** 规则行模型。M4 起由 domain 的 RuleEntry 映射。 */
data class RuleListItem(
    val id: String,
    val rawText: String,
    val sourceLabel: String,
    val hitCount: Long,
)

/**
 * 规则行。
 *
 * 每条规则显示来源标签与命中计数（提示词 5.11 的明确要求），
 * 因为规则冲突排查极度依赖「这条规则从哪来的」。
 */
@Composable
private fun RuleRow(rule: RuleListItem, modifier: Modifier = Modifier) {
    ListItem(
        modifier = modifier,
        headlineContent = {
            Text(
                text = rule.rawText,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        },
        supportingContent = {
            Text(
                text = "${
                    stringResource(R.string.rules_source_label)
                }: ${rule.sourceLabel}   ${
                    stringResource(R.string.rules_hit_count, rule.hitCount)
                }",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        },
    )
}
