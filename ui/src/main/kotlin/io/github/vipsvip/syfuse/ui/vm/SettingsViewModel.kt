package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.data.settings.AppSettingsDataStore
import io.github.vipsvip.syfuse.domain.settings.AppSettings
import io.github.vipsvip.syfuse.domain.settings.SettingGroup
import io.github.vipsvip.syfuse.domain.settings.SettingSpec
import io.github.vipsvip.syfuse.domain.settings.SettingType
import io.github.vipsvip.syfuse.domain.settings.SettingsCatalog
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 设置页状态。 */
data class SettingsUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val settings: AppSettings = AppSettings(),
    val query: String = "",
    val searchResults: List<SettingSpec> = emptyList(),
    /**
     * 待处理的动作项。UI 依据 `spec.requiresConfirm` 决定呈现形式：
     * 需要确认的弹二次确认对话框，其余弹详情/跳转。这样"危险操作二次确认"（7.3）
     * 由数据驱动，不依赖 UI 逐个判断。
     */
    val pendingAction: SettingSpec? = null,
) {
    val expertMode: Boolean get() = settings.getBoolean(KEY_EXPERT_MODE)

    /** 按分组取可见设置项。专家模式关闭时隐藏 expertOnly 项。 */
    fun visibleGroups(): List<Pair<SettingGroup, List<SettingSpec>>> =
        SettingsCatalog.groups().map { group ->
            group to SettingsCatalog.visibleIn(group, expertMode)
        }.filter { it.second.isNotEmpty() }

    companion object {
        const val KEY_EXPERT_MODE = "expert_mode"
    }
}

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val store: AppSettingsDataStore,
) : ViewModel() {

    private val _uiState = MutableStateFlow(SettingsUiState())
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    init {
        viewModelScope.launch {
            store.settings
                .onEach { settings ->
                    _uiState.value = _uiState.value.copy(loading = false, error = null, settings = settings)
                }
                .catch { t ->
                    _uiState.value = _uiState.value.copy(loading = false, error = t.message ?: "设置读取失败")
                }
                .collect { }
        }
    }

    fun onQueryChange(value: String) {
        // 搜索范围包含被专家模式隐藏的项——用户知道它存在就应该搜得到（5.10）
        _uiState.value = _uiState.value.copy(
            query = value,
            searchResults = SettingsCatalog.search(value),
        )
    }

    fun clearSearch() {
        _uiState.value = _uiState.value.copy(query = "", searchResults = emptyList())
    }

    /** 切换布尔项。 */
    fun toggle(spec: SettingSpec) {
        val current = _uiState.value.settings.get(spec.key) ?: spec.defaultValue
        val next = if (current == "true") "false" else "true"
        commit(spec, next)
    }

    /** 提交一个值；危险项先进入待确认状态。 */
    fun setValue(spec: SettingSpec, value: String) {
        commit(spec, value)
    }

    /** 动作项入口。 */
    fun requestAction(spec: SettingSpec) {
        _uiState.value = _uiState.value.copy(pendingAction = spec)
    }

    fun dismissAction() {
        _uiState.value = _uiState.value.copy(pendingAction = null)
    }

    /**
     * 用户已确认。
     * 对开关型项执行取反；对动作型项，实际动作由对应功能模块承担（各自里程碑交付），
     * 此处只负责关闭对话框——不假装动作已完成。
     */
    fun confirmPending() {
        val spec = _uiState.value.pendingAction ?: return
        if (spec.type == SettingType.TOGGLE) {
            val current = _uiState.value.settings.get(spec.key) ?: spec.defaultValue
            commit(spec, if (current == "true") "false" else "true")
        }
        dismissAction()
    }

    /** 恢复推荐值（7.3：所有专家级改动都可恢复推荐值）。 */
    fun resetToRecommended() {
        viewModelScope.launch { store.resetToDefaults() }
    }

    private fun commit(spec: SettingSpec, value: String) {
        viewModelScope.launch { store.set(spec.key, value) }
    }
}
