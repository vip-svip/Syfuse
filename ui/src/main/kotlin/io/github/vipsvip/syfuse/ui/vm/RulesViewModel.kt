package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.core.rule.FilterEngine
import io.github.vipsvip.syfuse.core.rule.MatchRequest
import io.github.vipsvip.syfuse.core.rule.MatchResult
import io.github.vipsvip.syfuse.core.rule.ResourceType
import io.github.vipsvip.syfuse.core.rule.RuleDiagnostic
import io.github.vipsvip.syfuse.core.rule.RuleParser
import io.github.vipsvip.syfuse.domain.model.RuleEntry
import io.github.vipsvip.syfuse.data.repo.ConfigRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * 规则页状态。
 *
 * 这里直接复用 core:rule 的解析器与引擎——UI 与规则语义共享**同一份实现**，
 * 不存在"界面显示的规则"与"内核执行的规则"两套解析逻辑的经典漂移问题。
 */
data class RulesUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val ruleCount: Int = 0,
    val indexedCount: Int = 0,
    val linearCount: Int = 0,
    val errors: List<RuleDiagnostic> = emptyList(),
    val warnings: List<RuleDiagnostic> = emptyList(),
    /** 规则测试的输入与结果。 */
    val testInput: String = "",
    val testAsUrl: Boolean = false,
    val testResult: MatchResult? = null,
    val testError: String? = null,
)

@HiltViewModel
class RulesViewModel @Inject constructor(
    private val repository: ConfigRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(RulesUiState())
    val uiState: StateFlow<RulesUiState> = _uiState.asStateFlow()

    private var engine: FilterEngine? = null

    init {
        viewModelScope.launch {
            repository.observeRuleEntries()
                .onEach { entries -> rebuild(entries) }
                .catch { t -> _uiState.value = _uiState.value.copy(loading = false, error = t.message) }
                .collect { }
        }
    }

    /** 从数据库里的规则文本重建引擎。规则源变更后自动重编译，等价于热重载。 */
    private fun rebuild(entries: List<RuleEntry>) {
        if (entries.isEmpty()) {
            engine = null
            _uiState.value = _uiState.value.copy(
                loading = false,
                ruleCount = 0,
                indexedCount = 0,
                linearCount = 0,
                errors = emptyList(),
                warnings = emptyList(),
                testResult = null,
            )
            return
        }

        val text = entries.joinToString("\n") { it.rawText }
        val report = RuleParser.parse(text, sourceId = "local")
        val compiled = FilterEngine.compile(report.networkRules, report.diagnostics)
        engine = compiled

        _uiState.value = _uiState.value.copy(
            loading = false,
            error = null,
            ruleCount = compiled.ruleCount,
            indexedCount = compiled.indexedRuleCount,
            linearCount = compiled.linearRuleCount,
            errors = report.errors,
            warnings = report.warnings,
        )
    }

    fun onTestInputChange(value: String) {
        _uiState.value = _uiState.value.copy(testInput = value)
    }

    fun setTestAsUrl(asUrl: Boolean) {
        _uiState.value = _uiState.value.copy(testAsUrl = asUrl)
    }

    /** 执行规则测试，产出完整匹配链路（5.4）。 */
    fun runTest() {
        val current = _uiState.value
        val input = current.testInput.trim()
        if (input.isEmpty()) {
            _uiState.value = current.copy(testError = "请输入域名或 URL", testResult = null)
            return
        }

        val active = engine
        if (active == null) {
            _uiState.value = current.copy(
                testError = "当前没有已导入的规则，请先在设置 → 规则中导入规则源",
                testResult = null,
            )
            return
        }

        val request = if (current.testAsUrl) {
            MatchRequest(
                host = MatchRequest.extractHost(input),
                url = input,
                resourceType = ResourceType.DOCUMENT,
            )
        } else {
            MatchRequest(host = input)
        }

        _uiState.value = current.copy(
            testResult = active.match(request, collectChain = true),
            testError = null,
        )
    }
}
