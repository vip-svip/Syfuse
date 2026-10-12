package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.data.repo.ConfigRepository
import io.github.vipsvip.syfuse.domain.engine.EngineController
import io.github.vipsvip.syfuse.domain.model.ProxyNode
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
    private val engine: EngineController,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    private val _testState = MutableStateFlow(TestState())

    private val nodes: StateFlow<List<ProxyNode>> = repository.observeNodes()
        .catch { }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val uiState: StateFlow<NodesUiState> =
        combine(nodes, _query, _testState) { list, q, test ->
            NodesUiState(
                loading = false,
                error = null,
                query = q,
                allNodes = list,
                testing = test.running,
                lastTestMessage = test.message,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), NodesUiState())

    fun onQueryChange(value: String) {
        _query.value = value
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
}
