package io.github.vipsvip.syfuse.ui.logs

import android.app.Application
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import dagger.hilt.android.EntryPointAccessors
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonClient
import io.github.vipsvip.syfuse.ui.components.EmptyState
import io.github.vipsvip.syfuse.ui.components.ErrorState
import io.github.vipsvip.syfuse.ui.components.LoadingState
import io.github.vipsvip.syfuse.ui.components.SectionHeader
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * 日志 页（M9「数据与统计」其二）。
 *
 * 数据来源：守护进程已实现的 `log.tail`（读 syfused.log 尾部）。
 * 本页只做"如实搬运"：守护进程读不到日志文件时，`log.tail` 会在应答里带
 * `note` 说明原因，这里原样展示，**不编造日志行**。
 *
 * 关于 [androidx.compose.ui.tooling.preview.Preview]：app 模块编译类路径上
 * 没有 `ui-tooling-preview`（已用 `:app:dependencies --configuration
 * debugCompileClasspath` 实测确认）。因此内容 composable 全部是
 * "普通数据类入参 + 无 IO + 无 Context" 的纯函数，补上该依赖后即可
 * 机械地外包一层 `@Preview`。
 */

/** Hilt 取件点：从 SingletonComponent 取 [DaemonClient]。 */
@EntryPoint
@InstallIn(SingletonComponent::class)
interface DaemonClientEntryPoint {
    fun daemonClient(): DaemonClient
}

/**
 * 日志页入口。导航只需挂这一行：`composable("logs") { LogsEntry() }`。
 *
 * 为什么不用 `@HiltViewModel` + `hiltViewModel()`：app 模块编译类路径上没有
 * hilt-navigation-compose（实测），ViewModel 装配约定在 `ui` 模块。
 * [DaemonClient] 由 `EngineModule` 以 Singleton 提供，按 Hilt 文档用
 * [EntryPointAccessors] 取它，不自建 socket、不改构建文件。
 */
@Composable
fun LogsEntry(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val client = remember(context) {
        val app = context.applicationContext as Application
        EntryPointAccessors.fromApplication(app, DaemonClientEntryPoint::class.java).daemonClient()
    }
    LogsScreen(client = client, modifier = modifier)
}

/** 单次读取的日志行数（守护进程 `log.tail` 的 lines 参数）。 */
private const val LOG_TAIL_LINES = 200

/** 日志页状态。三态显式分开。 */
private sealed interface LogsUiState {
    data object Loading : LogsUiState

    data class Unavailable(val error: SyfuseError) : LogsUiState

    /**
     * @param lines 守护进程如实返回的日志行；可能为空。
     * @param note 守护进程自查到的读取问题（如日志文件不存在）。为空表示没有额外说明。
     */
    data class Ready(val lines: List<String>, val note: String?) : LogsUiState
}

@Composable
fun LogsScreen(
    client: DaemonClient,
    modifier: Modifier = Modifier,
) {
    var uiState by remember { mutableStateOf<LogsUiState>(LogsUiState.Loading) }

    // 刷新令牌：右上角刷新按钮与错误态重试都 +1，重新发起一次 log.tail。
    var reloadToken by remember { mutableStateOf(0) }
    LaunchedEffect(client, reloadToken) {
        uiState = loadLogTail(client)
    }

    Column(modifier.fillMaxSize()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 0.dp, end = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionHeader("日志", Modifier.weight(1f))
            IconButton(
                onClick = { reloadToken++ },
                modifier = Modifier.semantics { contentDescription = "刷新日志" },
            ) {
                Icon(Icons.Filled.Refresh, contentDescription = null)
            }
        }

        when (val state = uiState) {
            LogsUiState.Loading -> LoadingState(
                message = "正在读取守护进程日志…",
                modifier = Modifier.weight(1f),
            )

            is LogsUiState.Unavailable -> ErrorState(
                userMessage = state.error.userMessage,
                technical = state.error.technical,
                hint = state.error.hint,
                modifier = Modifier.weight(1f),
                onRetry = { reloadToken++ },
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
                    onAction = { reloadToken++ },
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

private suspend fun loadLogTail(client: DaemonClient): LogsUiState =
    when (val result = client.tailLog(LOG_TAIL_LINES)) {
        is SyfuseResult.Success -> {
            val obj = result.value as? JsonObject
            val lines = (obj?.get("lines") as? JsonArray)
                ?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
                ?: emptyList()
            val note = (obj?.get("note") as? JsonPrimitive)?.contentOrNull?.takeIf { it.isNotBlank() }
            LogsUiState.Ready(lines = lines, note = note)
        }

        is SyfuseResult.Failure -> LogsUiState.Unavailable(result.error)
    }
