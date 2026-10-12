package io.github.vipsvip.syfuse.ui.page.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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

/**
 * 设置页（提示词 5.10 + 5.11）。
 *
 * 三项硬性要求都在此体现：
 * 1. 分组结构（[SettingsGroup]）
 * 2. 每个设置项三段式：标题 + 副标题说明 + 控件，并带风险等级标记
 * 3. 设置搜索必须有
 *
 * M1 阶段「通用」分组为完整实现样板，用于验证三段式布局与搜索交互；
 * 其余分组在各自里程碑填充（见 [SettingsGroup] 的 KDoc）。
 */
@Composable
fun SettingsPage(modifier: Modifier = Modifier) {
    var query by remember { mutableStateOf("") }
    // 专家模式开关。M1 用本地状态；M2 起接 DataStore
    var expertMode by remember { mutableStateOf(false) }

    val visibleGroups = remember(query, expertMode) {
        filterGroups(query, expertMode)
    }

    Column(modifier = modifier.fillMaxWidth()) {
        OutlinedTextField(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            value = query,
            onValueChange = { query = it },
            singleLine = true,
            label = { Text(stringResource(R.string.settings_search_hint)) },
        )

        if (visibleGroups.isEmpty()) {
            // 搜索无结果也是「空状态」的一种，必须有明确反馈而非空白
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(32.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(
                    text = stringResource(R.string.settings_no_match),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        } else {
            LazyColumn(modifier = Modifier.fillMaxWidth()) {
                visibleGroups.forEach { (group, items) ->
                    item(key = "header-${group.name}") {
                        GroupHeader(group)
                    }
                    items(items, key = { "${group.name}-${it.key}" }) { setting ->
                        SettingRow(setting)
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

/**
 * 按查询词与专家模式过滤分组。
 *
 * 抽为独立函数（而非内联在 Composable 里）的原因：这是纯逻辑，可以单测。
 * 提示词 2.8 要求关键算法必须有单测，设置搜索虽小但用户依赖度高，
 * 过滤错误会导致「设置项找不到」这类难排查的问题。
 */
internal fun filterGroups(
    query: String,
    expertMode: Boolean,
): List<Pair<SettingsGroup, List<SettingsItem>>> {
    val normalized = query.trim().lowercase()

    return SettingsGroup.entries.mapNotNull { group ->
        val groupTitleMatches = normalized.isNotEmpty() &&
            group.name.contains(normalized, ignoreCase = true)

        val matching = group.items.filter { item ->
            // 专家模式约束：非专家模式隐藏 expertOnly 项（提示词 5.10）
            if (item.expertOnly && !expertMode) return@filter false
            if (normalized.isEmpty()) return@filter true
            // 按 key 匹配，因为 key 是稳定英文标识，便于英文查询；
            // 中文查询依赖分组标题匹配（见 groupTitleMatches）
            item.key.contains(normalized, ignoreCase = true) || groupTitleMatches
        }

        if (matching.isEmpty()) null else group to matching
    }
}

@Composable
private fun GroupHeader(group: SettingsGroup) {
    Text(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        text = stringResource(group.titleRes),
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.primary,
    )
}

/**
 * 单个设置项行：标题 + 副标题 + 右侧控件占位 + 风险等级标记。
 *
 * M1 阶段控件区留空，因为设置项的控件类型（开关 / 下拉 / 滑块 / 跳转）
 * 取决于具体项，在「通用」分组的 M2 实现中接入。
 */
@Composable
private fun SettingRow(setting: SettingsItem) {
    ListItem(
        headlineContent = { Text(stringResource(setting.titleRes)) },
        supportingContent = {
            Text(
                text = stringResource(setting.descRes),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        },
        trailingContent = {
            RiskBadge(setting.risk)
        },
    )
}

/**
 * 风险等级徽标。
 *
 * 用 AssistChip 而非纯文字，是为了满足提示词 7.4 的触控目标要求，
 * 同时保证在浅色/深色/AMOLED 三种主题下都有足够对比度。
 */
@Composable
private fun RiskBadge(risk: RiskLevel) {
    val containerColor = when (risk) {
        RiskLevel.SAFE -> MaterialTheme.colorScheme.surfaceVariant
        RiskLevel.ADVANCED -> AdvancedContainer
        RiskLevel.DANGEROUS -> MaterialTheme.colorScheme.errorContainer
    }
    val labelColor = when (risk) {
        RiskLevel.SAFE -> MaterialTheme.colorScheme.onSurfaceVariant
        RiskLevel.ADVANCED -> AdvancedLabel
        RiskLevel.DANGEROUS -> MaterialTheme.colorScheme.onErrorContainer
    }

    AssistChip(
        onClick = { /* 危险项的解释弹窗在 M2 接入 */ },
        label = { Text(stringResource(risk.labelRes), style = MaterialTheme.typography.bodySmall) },
        colors = AssistChipDefaults.assistChipColors(
            containerColor = containerColor,
            labelColor = labelColor,
        ),
    )
}

/** 「进阶」用琥珀色：既区别于安全的灰、也区别于危险的红的语义色。 */
private val AdvancedContainer = Color(0xFFFFF3E0)
private val AdvancedLabel = Color(0xFF8D6E00)

/** 供测试引用的可见行数计算，避免测试重复实现过滤逻辑。 */
internal fun countVisibleItems(query: String, expertMode: Boolean): Int =
    filterGroups(query, expertMode).sumOf { it.second.size }
