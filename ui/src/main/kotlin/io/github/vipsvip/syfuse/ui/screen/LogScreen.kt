package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.vipsvip.syfuse.ui.components.EmptyState
import io.github.vipsvip.syfuse.ui.components.ErrorState
import io.github.vipsvip.syfuse.ui.components.LoadingState
import io.github.vipsvip.syfuse.ui.components.SectionHeader
import io.github.vipsvip.syfuse.ui.vm.LogsUiState
import io.github.vipsvip.syfuse.ui.vm.LogsViewModel

/**
 * 日志 页（M9「数据与统计」其二）。
 *
 * 数据来源：守护进程已实现的 `log.tail`（读 syfused.log 尾部）。
 * 本页只做"如实搬运"：守护进程读不到日志文件时，`log.tail` 会在应答里带
 * `note` 说明原因，这里原样展示，**不编造日志行**。
 *
 * **本页原先写在 `app` 模块且无法被导航到达**（依赖方向 `app → ui`，
 * 而导航树在 `ui`），属于"功能不生效"。现已迁到 `ui` 并在
 * `SyfuseApp.kt` 挂上 `composable("logs")`。原文件注释里写着
 * 「导航只需挂这一行：`composable("logs") { LogsEntry() }`」，
 * 但那一行从未被任何人写上 —— 这正是它成为死代码的原因。
 */

/** 日志页入口。已挂在导航路由 `"logs"` 上（`SyfuseApp.kt`）。 */
@Composable
fun LogsEntry(modifier: Modifier = Modifier) {
    val vm: LogsViewModel = hiltViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    LogsScreen(
        state = state,
        onRetry = { vm.refresh() },
        modifier = modifier,
    )
}

/**
 * 纯渲染：输入 state、输出 UI，不做 IO、不读 Context。
 * 这正是 `@Preview` 与 JVM 单测都能直接驱动的形态。
 */
@Composable
fun LogsScreen(
    state: LogsUiState,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxSize()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 0.dp, end = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionHeader("日志", Modifier.weight(1f))
            IconButton(
                onClick = onRetry,
                modifier = Modifier.semantics { contentDescription = "刷新日志" },
            ) {
                Icon(Icons.Filled.Refresh, contentDescription = null)
            }
        }

        when (state) {
            LogsUiState.Loading -> LoadingState(
                message = "正在读取守护进程日志…",
                modifier = Modifier.weight(1f),
            )

            is LogsUiState.Unavailable -> ErrorState(
                userMessage = state.error.userMessage,
                technical = state.error.technical,
                hint = state.error.hint,
                modifier = Modifier.weight(1f),
                onRetry = onRetry,
            )

            is LogsUiState.Ready -> when {
                state.lines.isEmpty() && state.note != null -> DaemonNoteCard(
                    note = state.note,
                    modifier = Modifier.weight(1f),
                )

                state.lines.isEmpty() -> EmptyState(
                    title = "暂无日志",
                    description = "守护进程还没有写出任何日志行。可以点右上角刷新再试。",
                    modifier = Modifier.weight(1f),
                    actionLabel = "刷新",
                    onAction = onRetry,
                )

                else -> LazyColumn(
                    Modifier
                        .weight(1f)
                        .padding(horizontal = 16.dp),
                    verticalArrangement = Arrangement.spacedBy(2.dp),
                ) {
                    items(state.lines) { line ->
                        Text(
                            text = line,
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                        )
                    }
                }
            }
        }
    }
}

/** 守护进程如实报告的读取问题（例如日志文件不存在）。原样展示。 */
@Composable
private fun DaemonNoteCard(note: String, modifier: Modifier = Modifier) {
    Card(
        modifier = modifier
            .fillMaxWidth()
            .padding(16.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(16.dp)) {
            Text("守护进程读不到日志", style = MaterialTheme.typography.titleMedium)
            Text(
                note,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
