package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.data.repo.ConfigRepository
import io.github.vipsvip.syfuse.domain.engine.EngineConfig
import io.github.vipsvip.syfuse.domain.engine.EngineController
import io.github.vipsvip.syfuse.domain.engine.EngineState
import io.github.vipsvip.syfuse.domain.engine.EngineStatus
import io.github.vipsvip.syfuse.domain.engine.TrafficRate
import io.github.vipsvip.syfuse.domain.model.Profile
import io.github.vipsvip.syfuse.domain.model.RoutingMode
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 首页状态。 */
data class HomeUiState(
    val status: EngineStatus = EngineStatus(),
    val rate: TrafficRate = TrafficRate(),
    val routingMode: RoutingMode = RoutingMode.RULE,
    val currentProfileName: String? = null,
    val currentNodeName: String? = null,
    /** 过滤规则拦截计数。位置在首页末尾，不干扰主干（5.1）。 */
    val blockedCount: Long = 0,
)

@HiltViewModel
class HomeViewModel @Inject constructor(
    private val engine: EngineController,
    private val repository: ConfigRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(HomeUiState())
    val uiState: StateFlow<HomeUiState> = _uiState.asStateFlow()

    init {
        // 速率采样：1 秒一次。用固定间隔而非事件驱动，因为内核上报本身是轮询式的。
        viewModelScope.launch {
            while (isActive) {
                val rate = engine.sampleRate()
                _uiState.value = _uiState.value.copy(rate = rate)
                delay(1000)
            }
        }
        viewModelScope.launch {
            engine.status.collect { status ->
                _uiState.value = _uiState.value.copy(status = status)
            }
        }
        viewModelScope.launch {
            repository.observeProfiles()
                .catch { }
                .collect { profiles ->
                    _uiState.value = _uiState.value.copy(
                        currentProfileName = profiles.firstOrNull()?.name,
                        routingMode = profiles.firstOrNull()?.routingMode ?: RoutingMode.RULE,
                    )
                }
        }
    }

    /** 顶部大开关。四态语义由 [EngineState] 表达。 */
    fun toggleConnection() {
        viewModelScope.launch {
            if (engine.status.value.state == EngineState.RUNNING) {
                engine.stop()
            } else {
                engine.start(buildConfig())
            }
        }
    }

    fun setRoutingMode(mode: RoutingMode) {
        _uiState.value = _uiState.value.copy(routingMode = mode)
    }

    private fun buildConfig(): EngineConfig {
        val profile = _uiState.value.currentProfileName?.let { name ->
            Profile(id = "current", name = name, routingMode = _uiState.value.routingMode)
        } ?: Profile(id = "default", name = "默认配置")
        // 内核配置文本由 M2 的配置编译器产出；此处传入空文本，
        // NotLinkedDataPlane 会在 start 时如实报"内核未接入"。
        return EngineConfig(profile = profile, coreConfig = "")
    }
}
