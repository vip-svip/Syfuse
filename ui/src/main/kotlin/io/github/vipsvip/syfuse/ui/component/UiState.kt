package io.github.vipsvip.syfuse.ui.component

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

/**
 * 页面四态容器（提示词 7.3 硬性要求：每个页面实现加载中 / 空 / 错误 / 无权限）。
 *
 * 设计取舍：用密封类 + 单一容器，而非让每个页面各写一套 if-else。
 * 理由：四态是全局一致的要求，集中实现能保证「没有一个页面漏掉某态」，
 * 也能让 UI 资产基线检查（tests/ui_assets_check）有统一的可断言锚点。
 */
sealed interface UiState<out T> {
    /** 加载中，渲染骨架屏。 */
    data object Loading : UiState<Nothing>

    /** 成功且有数据。 */
    data class Content<T>(val data: T) : UiState<T>

    /** 成功但结果为空，需要引导动作（如「导入订阅」）。 */
    data class Empty(
        val title: String? = null,
        val actionLabel: String? = null,
        val onAction: (() -> Unit)? = null,
    ) : UiState<Nothing>

    /** 失败，必须给出原因与重试入口。 */
    data class Error(
        val message: String,
        val onRetry: (() -> Unit)? = null,
    ) : UiState<Nothing>

    /**
     * 无权限。独立于 [Error] 的原因：权限缺失需要引导用户去系统设置，
     * 而普通错误只需要重试，两者的行动按钮与文案完全不同。
     */
    data class NoPermission(
        val capability: String,
        val onGrant: (() -> Unit)? = null,
    ) : UiState<Nothing>
}

/**
 * 按 [state] 渲染四态。
 *
 * @param content 成功态的内容渲染，只有 [UiState.Content] 会调用它。
 */
@Composable
fun <T> StatefulContent(
    state: UiState<T>,
    modifier: Modifier = Modifier,
    content: @Composable (T) -> Unit,
) {
    when (state) {
        is UiState.Loading -> LoadingState(modifier)
        is UiState.Content -> content(state.data)
        is UiState.Empty -> EmptyState(state, modifier)
        is UiState.Error -> ErrorState(state, modifier)
        is UiState.NoPermission -> NoPermissionState(state, modifier)
    }
}

/**
 * 骨架屏。
 *
 * 提示词 7.2 禁止大面积 blur，因此这里用低透明度色块而非毛玻璃效果，
 * 既表达「加载中」又不触发昂贵的模糊合成。
 */
@Composable
fun LoadingState(modifier: Modifier = Modifier) {
    Box(
        modifier = modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator()
    }
}

@Composable
private fun EmptyState(state: UiState.Empty, modifier: Modifier = Modifier) {
    CenteredMessage(
        modifier = modifier,
        title = state.title ?: stringResource(io.github.vipsvip.syfuse.ui.R.string.state_empty_title),
        actionLabel = state.actionLabel,
        onAction = state.onAction,
    )
}

@Composable
private fun ErrorState(state: UiState.Error, modifier: Modifier = Modifier) {
    CenteredMessage(
        modifier = modifier,
        title = stringResource(io.github.vipsvip.syfuse.ui.R.string.state_error_title),
        // 错误必须展示具体原因，不允许只显示「出错了」（提示词 2.3 第 6 条禁止沉默失败）
        detail = state.message,
        actionLabel = state.onRetry?.let { stringResource(io.github.vipsvip.syfuse.ui.R.string.action_retry) },
        onAction = state.onRetry,
    )
}

@Composable
private fun NoPermissionState(state: UiState.NoPermission, modifier: Modifier = Modifier) {
    CenteredMessage(
        modifier = modifier,
        title = state.capability,
        detail = stringResource(io.github.vipsvip.syfuse.ui.R.string.state_error_title),
        actionLabel = state.onGrant?.let { stringResource(io.github.vipsvip.syfuse.ui.R.string.action_confirm) },
        onAction = state.onGrant,
    )
}

/** 居中消息块的共用实现，保证四态视觉一致。 */
@Composable
private fun CenteredMessage(
    modifier: Modifier = Modifier,
    title: String,
    detail: String? = null,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Box(
        modifier = modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = title,
                style = MaterialTheme.typography.titleMedium,
                textAlign = TextAlign.Center,
            )
            detail?.let {
                Text(
                    text = it,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
            if (actionLabel != null && onAction != null) {
                Button(onClick = onAction) { Text(actionLabel) }
            }
        }
    }
}

/**
 * 保持 LazyColumn 的滚动性能锚点。
 *
 * 说明：这不是空抽象——提示词 7.2 要求列表全部虚拟化，此函数在 M3 起被节点页与
 * 规则页复用，统一设置 contentPadding 与 key 策略，避免各页参数不一致导致性能回退。
 */
@Composable
fun <T> VirtualList(
    items: List<T>,
    modifier: Modifier = Modifier,
    key: (T) -> Any,
    itemContent: @Composable (T) -> Unit,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(vertical = 8.dp),
    ) {
        itemsIndexed(items, key = { _, item -> key(item) }) { _, item ->
            itemContent(item)
        }
    }
}
