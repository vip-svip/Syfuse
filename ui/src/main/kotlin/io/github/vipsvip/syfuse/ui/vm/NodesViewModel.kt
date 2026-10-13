package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.data.repo.ConfigRepository
import io.github.vipsvip.syfuse.data.repo.SubscriptionRepository
import io.github.vipsvip.syfuse.domain.engine.EngineController
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.domain.node.SubscriptionImport
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 节点页状态。 */
data class NodesUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val query: String = "",
    val allNodes: List<ProxyNode> = emptyList(),
    val testing: Boolean = false,
    val lastTestMessage: String? = null,
    /** 订阅导入是否进行中。 */
    val importing: Boolean = false,
    /** 最近一次导入的结果摘要（成功给数字，失败给原因与建议）。 */
    val importMessage: String? = null,
    /** 导入弹窗是否可见。 */
    val importDialogVisible: Boolean = false,
) {
    /** 过滤后的可见节点。搜索命中名称、服务器与协议。 */
    val visibleNodes: List<ProxyNode>
        get() = if (query.isBlank()) {
            allNodes
        } else {
            allNodes.filter {
                it.name.contains(query, ignoreCase = true) ||
                    it.server.contains(query, ignoreCase = true) ||
                    it.protocol.name.contains(query, ignoreCase = true)
            }
        }
}

@HiltViewModel
class NodesViewModel @Inject constructor(
    private val repository: ConfigRepository,
    private val subscriptions: SubscriptionRepository,
    private val engine: EngineController,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    private val _testState = MutableStateFlow(TestState())
    private val _importState = MutableStateFlow(ImportState())

    private val nodes: StateFlow<List<ProxyNode>> = repository.observeNodes()
        .catch { }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val uiState: StateFlow<NodesUiState> =
        combine(nodes, _query, _testState, _importState) { list, q, test, imp ->
            NodesUiState(
                loading = false,
                error = null,
                query = q,
                allNodes = list,
                testing = test.running,
                lastTestMessage = test.message,
                importing = imp.running,
                importMessage = imp.message,
                importDialogVisible = imp.dialogVisible,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), NodesUiState())

    fun onQueryChange(value: String) {
        _query.value = value
    }

    // ------------------------------------------------------------------ 导入

    fun showImportDialog() {
        _importState.value = _importState.value.copy(dialogVisible = true, message = null)
    }

    fun dismissImportDialog() {
        _importState.value = _importState.value.copy(dialogVisible = false)
    }

    /** 清掉导入提示（用户已读）。 */
    fun consumeImportMessage() {
        _importState.value = _importState.value.copy(message = null)
    }

    /**
     * 从订阅链接导入。
     *
     * 失败时**不**静默：把"用户看到什么"与"建议做什么"一起显示出来。
     * 空链接是用户操作失误，不当成异常。
     */
    fun importFromUrl(url: String) {
        val target = url.trim()
        if (target.isEmpty()) {
            _importState.value = _importState.value.copy(message = "请先填写订阅链接")
            return
        }
        if (_importState.value.running) return
        _importState.value = _importState.value.copy(running = true, message = null)
        viewModelScope.launch {
            val message = when (val r = subscriptions.importFromUrl(target)) {
                is SyfuseResult.Success -> buildImportSummary(r.value)
                is SyfuseResult.Failure -> "导入失败：${r.error.userMessage}" +
                    (r.error.hint?.let { "\n$it" } ?: "")
            }
            _importState.value = _importState.value.copy(running = false, message = message)
        }
    }

    /**
     * 导入**手动粘贴**的订阅内容。
     *
     * 与 [importFromUrl] 的差别：没有来源标签，因此不会清理既有节点 ——
     * 粘贴一段文字的意图是"加进来"，不是"用这份替换那个订阅"。
     */
    fun importFromText(text: String) {
        if (text.isBlank()) {
            _importState.value = _importState.value.copy(message = "请先粘贴订阅内容")
            return
        }
        if (_importState.value.running) return
        _importState.value = _importState.value.copy(running = true, message = null)
        viewModelScope.launch {
            val message = when (val r = subscriptions.importFromContent(text)) {
                is SyfuseResult.Success -> buildImportSummary(r.value)
                is SyfuseResult.Failure -> "导入失败：${r.error.userMessage}" +
                    (r.error.hint?.let { "\n$it" } ?: "")
            }
            _importState.value = _importState.value.copy(running = false, message = message)
        }
    }

    private fun buildImportSummary(plan: SubscriptionImport.Plan): String = buildString {
        append("导入成功：${plan.totalCount} 个节点")
        if (plan.newCount > 0) append("（新增 ${plan.newCount}）")
        if (plan.updatedCount > 0) append("（更新 ${plan.updatedCount}）")
        if (plan.removedCount > 0) append("，清理已下线 ${plan.removedCount} 个")
        if (plan.lineErrorCount > 0) append("，${plan.lineErrorCount} 行无法解析")
    }

    /**
     * 批量测速。
     * 内核未接入时**不会**写入任何延迟数字——宁可显示"未接入"也不伪造延迟（R4）。
     */
    fun urlTestAll() {
        val names = uiState.value.allNodes.map { it.name }
        if (names.isEmpty()) {
            _testState.value = TestState(running = false, message = "没有可测速的节点")
            return
        }
        _testState.value = TestState(running = true, message = null)
        viewModelScope.launch {
            _testState.value = when (val result = engine.urlTest(names)) {
                is SyfuseResult.Success -> TestState(
                    running = false,
                    message = "测速完成：${result.value.count { it.value != null }}/${names.size} 个节点有响应",
                )
                is SyfuseResult.Failure -> TestState(
                    running = false,
                    message = result.error.userMessage + (result.error.hint?.let { "（$it）" } ?: ""),
                )
            }
        }
    }

    private data class TestState(val running: Boolean = false, val message: String? = null)
    private data class ImportState(
        val running: Boolean = false,
        val message: String? = null,
        val dialogVisible: Boolean = false,
    )
}
