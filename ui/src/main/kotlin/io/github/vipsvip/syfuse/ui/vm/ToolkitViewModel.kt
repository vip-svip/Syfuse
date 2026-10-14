package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonClient
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import javax.inject.Inject

/**
 * 工具箱（拦截页）状态：M10–M13 新能力的 UI 出口。
 *
 * 每个卡片独立 loading/error，互不阻塞 —— 某一项失败（如守护未连）
 * 不能让整页瘫掉。数据全部来自真实 UDS 调用，没有任何假数据。
 */
data class ToolkitUiState(
    /** 过滤引擎统计（filter.stats）。 */
    val filterStats: JsonObject? = null,
    /** Hook 框架检测（hook.status）。 */
    val hookStatus: JsonObject? = null,
    /** 防泄漏一键修复结果（leak.fix）。 */
    val leakFix: JsonObject? = null,
    /** 通知净化试跑结果（notify.test）。 */
    val notifyTest: JsonObject? = null,
    /** 备份包路径（backup.create）。 */
    val backupPath: String? = null,
    /** 正在执行的卡片名（同一时刻一个），空串 = 空闲。 */
    val busy: String = "",
    /** 最近一次错误（卡片名 → 消息）。 */
    val error: Map<String, String> = emptyMap(),
    /** 守护连接是否可用（首次探测）。 */
    val daemonOk: Boolean? = null,
)

@HiltViewModel
class ToolkitViewModel @Inject constructor(
    private val client: DaemonClient,
) : ViewModel() {

    private val json = Json { ignoreUnknownKeys = true }

    private val _uiState = MutableStateFlow(ToolkitUiState())
    val uiState: StateFlow<ToolkitUiState> = _uiState.asStateFlow()

    init {
        // 首帧即拉过滤统计与 Hook 状态（两个只读探测，失败静默记 error）。
        refreshFilter()
        refreshHook()
    }

    private fun set(transform: (ToolkitUiState) -> ToolkitUiState) {
        _uiState.value = transform(_uiState.value)
    }

    private fun setError(card: String, message: String?) {
        set { st ->
            val errs = st.error.toMutableMap()
            if (message == null) errs.remove(card) else errs[card] = message
            st.copy(error = errs, busy = "")
        }
    }

    private suspend fun callDaemon(method: String, params: String? = null): Result<JsonElement> =
        runCatching {
            val element = params?.let {
                client.call(method, json.parseToJsonElement(it))
            } ?: client.call(method)
            when (element) {
                is SyfuseResult.Success -> element.value
                is SyfuseResult.Failure ->
                    throw IllegalStateException(element.error.userMessage.ifBlank { element.error.technical })
            }
        }

    /** 过滤引擎统计。 */
    fun refreshFilter() {
        set { it.copy(busy = "filter") }
        viewModelScope.launch {
            callDaemon("filter.stats")
                .onSuccess { el ->
                    set { it.copy(filterStats = el as? JsonObject, busy = "") }
                }
                .onFailure { setError("filter", it.message) }
        }
    }

    /** config.reload：热重载过滤/通知规则。 */
    fun reloadConfig() {
        set { it.copy(busy = "reload") }
        viewModelScope.launch {
            callDaemon("config.reload")
                .onSuccess {
                    setError("reload", null)
                    refreshFilter()
                }
                .onFailure { setError("reload", it.message) }
        }
    }

    /** Hook 框架检测。 */
    fun refreshHook() {
        set { it.copy(busy = "hook") }
        viewModelScope.launch {
            callDaemon("hook.status")
                .onSuccess { el ->
                    set { it.copy(hookStatus = el as? JsonObject, busy = "") }
                }
                .onFailure { setError("hook", it.message) }
        }
    }

    /** 防泄漏自检 + 一键修复。 */
    fun leakFix() {
        set { it.copy(busy = "leak") }
        viewModelScope.launch {
            callDaemon("leak.fix")
                .onSuccess { el ->
                    set { it.copy(leakFix = el as? JsonObject, busy = "") }
                }
                .onFailure { setError("leak", it.message) }
        }
    }

    /** 通知净化单条试跑。 */
    fun notifyTest(pkg: String, title: String, content: String) {
        set { it.copy(busy = "notify") }
        viewModelScope.launch {
            val params = """{"pkg":"${esc(pkg)}","title":"${esc(title)}","content":"${esc(content)}"}"""
            callDaemon("notify.test", params)
                .onSuccess { el ->
                    set { it.copy(notifyTest = el as? JsonObject, busy = "") }
                }
                .onFailure { setError("notify", it.message) }
        }
    }

    /** 创建备份包。 */
    fun backupCreate() {
        set { it.copy(busy = "backup") }
        viewModelScope.launch {
            callDaemon("backup.create")
                .onSuccess { el ->
                    val path = (el as? JsonObject)?.get("zip")?.toString()?.trim('"')
                    set { it.copy(backupPath = path, busy = "") }
                }
                .onFailure { setError("backup", it.message) }
        }
    }

    private fun esc(s: String): String =
        s.replace("\\", "\\\\").replace("\"", "\\\"").replace("\n", "\\n")
}
