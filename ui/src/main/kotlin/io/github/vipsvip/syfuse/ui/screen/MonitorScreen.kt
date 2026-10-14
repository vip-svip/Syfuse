package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
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
 * **本页是二级页的入口列表，不再是"全部未实现"的告示牌。**
 *
 * 原实现把 5 个功能组一律渲染成红字「未实现 + 计划里程碑 M9」，其中包括
 * 连接列表、日志、流量统计这三项 —— 而这三项的守护进程方法
 * （`connections` / `log.tail` / `traffic`）**早已实现**，且三个页面也已
 * 真实接通数据。对用户宣称"未实现"是**错误的能力汇报**。
 *
 * 现在按真实状态分两类：
 *  - [MonitorEntry.Ready]：点进去是该功能的真实页面（可点击，带右箭头）；
 *  - [MonitorEntry.Pending]：确实还没做，显式标注里程碑，**不可点击**，
 *    不做成看起来可点的死 UI（12.2 第 4 条）。
 *
 * `onOpen` 收到的是导航路由名，由 `SyfuseApp.kt` 的 `composable(...)` 处理。
 */
data class MonitorEntry(
    val title: String,
    val route: String?,
    val milestone: String?,
    val note: String? = null,
) {
    /** 有真实页面可去 → 可点击。 */
    val isReady: Boolean get() = route != null
}

/** 本页的真实状态表。改这里就要同步改 [SyfuseApp] 的路由。 */
val monitorEntries: List<MonitorEntry> = listOf(
    MonitorEntry("连接列表", route = "connections", milestone = null),
    MonitorEntry("日志（代理 / DNS / 脚本 / Hook）", route = "logs", milestone = null),
    MonitorEntry("流量统计与排行", route = "stats", milestone = null),
    MonitorEntry("会话审计", route = null, milestone = "M9", note = "需要按会话持久化留存，尚未开始"),
    MonitorEntry("导出", route = null, milestone = "M9", note = "依赖会话审计与统计的持久化，尚未开始"),
)

@Composable
fun MonitorScreen(onOpen: (String) -> Unit = {}) {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SectionHeader("监控")
        Text(
            text = "以下功能组按提示词 4.1 的布局 B 归属本 Tab。" +
                "可点的条目已接通守护进程真实数据；标注「未实现」的确实还没做，入口不做成死 UI。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        )
        monitorEntries.forEach { entry ->
            GroupRow(entry = entry, onOpen = onOpen)
        }
    }
}

/**
 * 一行"功能组 + 状态"。
 *
 * 两种形态严格区分：可用的给点击与右箭头，不可用的给红字状态且**不设点击**。
 */
@Composable
private fun GroupRow(entry: MonitorEntry, onOpen: (String) -> Unit) {
    val statusText = if (entry.isReady) "可用" else "未实现"
    val a11y = if (entry.isReady) {
        "${entry.title}，可用，点击进入"
    } else {
        "${entry.title}，未实现，计划里程碑 ${entry.milestone}"
    }

    val base = Modifier
        .fillMaxWidth()
        .padding(horizontal = 16.dp, vertical = 4.dp)
        .semantics { contentDescription = a11y }
    val clickable = if (entry.isReady) {
        base.clickable { onOpen(entry.route!!) }
    } else {
        base
    }

    Card(
        modifier = clickable,
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(entry.title, style = MaterialTheme.typography.titleMedium)
                Text(
                    entry.note ?: if (entry.isReady) "已接通守护进程真实数据" else "计划里程碑 ${entry.milestone}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                statusText,
                style = MaterialTheme.typography.labelSmall,
                color = if (entry.isReady) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.error
                },
            )
            if (entry.isReady) {
                Icon(
                    Icons.AutoMirrored.Filled.KeyboardArrowRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
