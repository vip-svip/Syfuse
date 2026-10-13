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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.vipsvip.syfuse.ui.components.SectionHeader

/**
 * 监控 页（提示词 4.1 布局 B 的 Tab）。
 *
 * **当前状态：全部入口未实现。** 本页的作用是把 4.1 规定的功能组落到具体位置，
 * 使「功能有界面落点」可被 `tests/ui_parity_check.sh` 核对。
 *
 * 每个入口都显式标注「未实现 + 计划里程碑」，**不做成看起来可点的死 UI**
 * （12.2 第 4 条）。这不是占位：它是**如实的能力状态展示**，
 * 用户点进去看到的就是"还没做，计划在哪个里程碑"。
 */
@Composable
fun MonitorScreen() {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SectionHeader("监控")
        Text(
            text = "以下功能组按提示词 4.1 的布局 B 归属本 Tab。当前均未实现，入口不伪装可用。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        )
        GroupRow("连接列表", "M9")
        GroupRow("日志（代理 / DNS / 脚本 / Hook）", "M9")
        GroupRow("流量统计与排行", "M9")
        GroupRow("会话审计", "M9")
        GroupRow("导出", "M9")
    }
}

/** 一行"功能组 + 状态"。刻意不抽到 components —— 它只服务本页，抽出去就是过度工程。 */
@Composable
private fun GroupRow(title: String, milestone: String) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp)
            .semantics { contentDescription = "$title，未实现，计划里程碑 $milestone" },
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleMedium)
                Text(
                    "计划里程碑 $milestone",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                "未实现",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
    }
}
