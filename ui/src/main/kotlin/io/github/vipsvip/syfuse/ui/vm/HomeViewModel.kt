package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.core.common.SyfuseResult
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
    /** 快捷动作的一次性反馈文本；null 表示无需展示。 */
    val quickActionMessage: String? = null,
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
        // 节点列表：原实现从不写 currentNodeName，导致首页永远显示「未选择节点」，
        // 而「测速」又必须有节点名才能调用内核。这里补上唯一的数据来源。
        viewModelScope.launch {
            repository.observeNodes()
                .catch { }
                .collect { nodes ->
                    _uiState.value = _uiState.value.copy(
                        currentNodeName = nodes.firstOrNull()?.name,
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

    /**
     * 快捷动作「测速」。真实调用内核的 `proxies.urlTest`，失败时回显原因。
     *
     * 纪律与 [io.github.vipsvip.syfuse.ui.vm.NodesViewModel] 一致：
     * 宁可显示"未接入"，也不伪造一个看着合理的延迟数字。
     */
    fun urlTest() {
        viewModelScope.launch {
            val node = _uiState.value.currentNodeName
            if (node == null) {
                _uiState.value = _uiState.value.copy(
                    quickActionMessage = "测速需要先导入节点",
                )
                return@launch
            }
            val message = when (val result = engine.urlTest(listOf(node))) {
                is SyfuseResult.Success ->
                    result.value[node]?.let { "$node 延迟 $it ms" } ?: "$node 未返回延迟"
                is SyfuseResult.Failure -> "测速失败：${result.error.userMessage}"
            }
            _uiState.value = _uiState.value.copy(quickActionMessage = message)
        }
    }

    private fun buildConfig(): EngineConfig {
        val profile = _uiState.value.currentProfileName?.let { name ->
            Profile(id = "current", name = name, routingMode = _uiState.value.routingMode)
        } ?: Profile(id = "default", name = "默认配置")
        // 内核配置文本由配置编译器产出（随 M5 交付）；此处仍传入空文本。
        // 注意：守护进程的 proxies.start **已经实现**（会真的拉起 SOCKS5 监听），
        // 所以它不会回"未实现（计划里程碑 M3）"——旧注释已过时。真正拦住连接的是
        // 更上游一环：无 root 时读不到 0600 的 api.token，EngineController 因此落在
        // ERROR 并展示"数据面不可用"。此处留空文本是已知缺口，不是"如实上报未实现"。
        return EngineConfig(profile = profile, coreConfig = "")
    }
}
