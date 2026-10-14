package io.github.vipsvip.syfuse.ui.vm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonClient
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import javax.inject.Inject

/**
 * 连接页 / 流量统计页 / 日志页的数据 ViewModel。
 *
 * **为什么这几个页面要搬到 `ui` 模块**（这是一个真实缺陷的修复，不是整理）：
 *
 * 这三个页面原本写在 `app` 模块（`app/src/main/kotlin/.../ui/{connections,stats,logs}/`），
 * 而导航树 `SyfuseApp` 住在 `ui` 模块。依赖方向是 `app → ui`
 * （`app/build.gradle.kts: implementation(project(":ui"))`），`ui` **无法**反向
 * import `app` 的类 —— 于是这三个页面**没有任何地方能引用它们**，全仓 grep
 * 只命中它们自身的定义。它们编译通过、单测通过、却永远不会被任一用户看到：
 * 典型的「功能不生效」。
 *
 * 搬到 `ui` 后同时获得两个好处：
 *  1. 导航可以真的挂上它们（`SyfuseApp.kt` 里的 `composable("connections")` 等）；
 *  2. `ui` 模块有 `hilt-navigation-compose` 与 JUnit5 + Truth，于是可以用
 *     标准的 `@HiltViewModel` + `hiltViewModel()` 装配，并给解析逻辑补单测。
 *     （原 `app` 模块既没有 hilt-navigation-compose 也没有任何测试目录，
 *     这正是那几个布尔解析 bug 一直没被发现的原因。）
 */
@HiltViewModel
class ConnectionsViewModel @Inject constructor(
    private val client: DaemonClient,
) : ViewModel() {

    private val _state = MutableStateFlow<ConnectionsUiState>(ConnectionsUiState.Loading)
    val state: StateFlow<ConnectionsUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        _state.value = ConnectionsUiState.Loading
        viewModelScope.launch {
            _state.value = loadConnections(client)
        }
    }
}

@HiltViewModel
class StatsViewModel @Inject constructor(
    private val client: DaemonClient,
) : ViewModel() {

    private val _state = MutableStateFlow<StatsUiState>(StatsUiState.Loading)
    val state: StateFlow<StatsUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        _state.value = StatsUiState.Loading
        viewModelScope.launch {
            _state.value = loadStats(client)
        }
    }
}

@HiltViewModel
class LogsViewModel @Inject constructor(
    private val client: DaemonClient,
) : ViewModel() {

    private val _state = MutableStateFlow<LogsUiState>(LogsUiState.Loading)
    val state: StateFlow<LogsUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        _state.value = LogsUiState.Loading
        viewModelScope.launch {
            _state.value = loadLogTail(client)
        }
    }
}

/**
 * 读取 JSON 原始字面量的**文本**表示。
 *
 * **为何不用 `contentOrNull`**：`JsonPrimitive.contentOrNull` 对**非字符串**
 * 字面量（布尔、数字）返回 null —— 它回答的是"这是一个 JSON 字符串吗"，
 * 而不是"这个字面量写了什么"。而守护进程的 `kernelLinked` / `running` 是
 * **布尔**（Go `src/cmd/syfused/proxies.go: "kernelLinked": isLinked(state)`，
 * `isLinked` 返回 bool），`nodeCount` / `uptimeSec` 是**数字**。
 * 用 `contentOrNull` 读它们会**恒得 null**，界面上就永远显示"未上报"，
 * 把一项真实能力伪装成不存在 —— 这正是本次修掉的缺陷之一。
 *
 * `.content` 对任意字面量都返回其文本（`true` / `6` / `"x"`），这正是这里要的。
 * 仓库内既有代码同样遵循这条约定：`ui/.../screen/InterceptScreen.kt:232` 用
 * `?.content == "true"` 读布尔；`daemon-protocol/.../DaemonWireRoundTripTest.kt:302`
 * 用 `jsonPrimitive.content` 断言布尔字段。
 *
 * `JsonNull` 仍返回 null（"守护进程没上报"），语义保持"缺失即缺失"。
 */
internal fun JsonObject.primitiveOrNull(key: String): String? =
    (this[key] as? JsonPrimitive)
        ?.takeIf { it !is JsonNull }
        ?.content

/** 字符串字段：只接受 JSON 字符串，避免把 `true` 当主机名之类的误用。 */
internal fun JsonObject.stringOrNull(key: String): String? =
    (this[key] as? JsonPrimitive)?.takeIf { it.isString }?.content

internal fun JsonObject.primitiveArrayOrNull(key: String): JsonArray? = this[key] as? JsonArray

// ---------------------------------------------------------------------------
// 连接页（connections + proxies.status）
// ---------------------------------------------------------------------------

/** 连接页状态。三态显式分开，不用 null 表达"加载中"或"出错"。 */
sealed interface ConnectionsUiState {
    data object Loading : ConnectionsUiState

    /** 守护进程不可达 / 返回了结构化错误。原因必须可展示、可重试。 */
    data class Unavailable(val error: SyfuseError) : ConnectionsUiState

    data class Ready(
        val summary: ProxySummary,
        val connections: List<ConnRow>,
        val connectionsError: SyfuseError?,
    ) : ConnectionsUiState
}

/**
 * 一条连接的可展示投影。
 *
 * 字段全部来自 Go 侧 `conn.Record`（`src/internal/conn/record.go` 的冻结契约）；
 * 守护进程没上报的保持 null，界面显示"未上报" —— 特别是 [dstIp] /
 * [srcApp] / [ruleText] / [chain]：内核当前不上报它们，界面上必须如实标注，
 * 而不是把 dstHost 抄进 dstIp 装作"有 IP"。
 */
data class ConnRow(
    val id: String,
    val network: String,
    val type: String,
    val dstHost: String?,
    val dstIp: String?,
    val srcApp: String?,
    val ruleText: String?,
    val chain: String?,
    val upBytes: Long,
    val downBytes: Long,
    val startAt: Long,
    val active: Boolean,
    val closeReason: String?,
)

/**
 * `proxies.status` 的展示子集。
 *
 * 每个字段都可为 null —— 那意味着"守护进程没有上报它"。
 * 界面据此显示"未上报"，而不是填一个看起来合理的默认值。
 */
data class ProxySummary(
    val state: String?,
    val selected: String?,
    val nodeCount: Int?,
    val socksAddr: String?,
    val uptimeSec: Long?,
    val kernelLinked: Boolean?,
)

internal suspend fun loadConnections(client: DaemonClient): ConnectionsUiState {
    val summary = when (val result = client.call("proxies.status")) {
        is SyfuseResult.Success -> parseProxySummary(result.value)
            ?: return ConnectionsUiState.Unavailable(
                SyfuseError(
                    code = ErrorCode.ENGINE_IPC_FAILED,
                    technical = "proxies.status 返回非预期格式: ${result.value}",
                    userMessage = "守护进程返回了无法解读的代理状态",
                    hint = "App 与模块版本可能不匹配",
                ),
            )

        is SyfuseResult.Failure -> return ConnectionsUiState.Unavailable(result.error)
    }

    // 连接列表是附加段：读不到不影响上面的代理概况，但要如实把原因带出来，
    // 让"读不到"和"当前没有连接"在界面上不可能被混淆。
    val connectionsResult = client.call("connections")
    val connections = (connectionsResult as? SyfuseResult.Success)
        ?.let { parseConnections(it.value) }
        ?: emptyList()
    val connectionsError = (connectionsResult as? SyfuseResult.Failure)?.error

    return ConnectionsUiState.Ready(
        summary = summary,
        connections = connections,
        connectionsError = connectionsError,
    )
}

/**
 * 解析 `connections` 应答（`{"records":[...],"stats":{...}}`）。
 *
 * 字段名与 Go 侧 `conn.Record` 一一对应。`endTime` 的零值
 * （"0001-01-01T00:00:00Z"）含义是**仍活动**，不能当成一个 0001 年的关闭时间。
 * `labels.decision` 是路由决策，映射到展示用的 type。
 */
internal fun parseConnections(element: JsonElement): List<ConnRow> {
    val obj = element as? JsonObject ?: return emptyList()
    val records = obj["records"] as? JsonArray ?: return emptyList()
    return records.mapNotNull { el ->
        val rec = el as? JsonObject ?: return@mapNotNull null
        val id = rec.primitiveOrNull("id") ?: return@mapNotNull null
        val endTime = rec.primitiveOrNull("endTime")
        val active = endTime == null || endTime.startsWith("0001-01-01")
        ConnRow(
            id = id,
            network = rec.stringOrNull("protocol") ?: "",
            type = (rec["labels"] as? JsonObject)?.stringOrNull("decision") ?: "",
            dstHost = rec.stringOrNull("dstHost"),
            // 内核侧没有独立的目标 IP 字段：绝不拿 srcAddr 冒充。
            dstIp = null,
            // UID→包名需要真机 TrafficStats，不编造。
            srcApp = null,
            ruleText = null,
            chain = null,
            upBytes = rec.primitiveOrNull("bytesIn")?.toLongOrNull() ?: 0L,
            downBytes = rec.primitiveOrNull("bytesOut")?.toLongOrNull() ?: 0L,
            startAt = rec.stringOrNull("startTime")?.let { parseEpochMillis(it) } ?: 0L,
            active = active,
            closeReason = rec.stringOrNull("closeReason")?.takeIf { it.isNotBlank() },
        )
    }
}

/** ISO-8601 时间串 → epoch 毫秒；解析不了回 0（"时间未知"）。 */
internal fun parseEpochMillis(text: String): Long =
    runCatching { java.time.Instant.parse(text).toEpochMilli() }.getOrDefault(0L)

/**
 * 解析 `proxies.status` 应答中本页需要的字段。
 *
 * 与 Go 侧 `handleProxiesStatus` 的字段逐一对应；缺失即 null，**不补默认值**。
 *
 * **`kernelLinked` 是布尔**（Go `proxies.go: "kernelLinked": isLinked(state)`），
 * 因此必须走 [primitiveOrNull] 取 `.content` 再转 Boolean —— 用 `contentOrNull`
 * 会恒得 null，界面就永远显示"未上报"，把"内核已接通"这一真实状态藏起来。
 */
internal fun parseProxySummary(element: JsonElement): ProxySummary? {
    val obj = element as? JsonObject ?: return null
    return ProxySummary(
        state = obj.stringOrNull("state"),
        selected = obj.stringOrNull("selected"),
        nodeCount = obj.primitiveOrNull("nodeCount")?.toIntOrNull(),
        socksAddr = obj.stringOrNull("socksAddr"),
        uptimeSec = obj.primitiveOrNull("uptimeSec")?.toLongOrNull(),
        kernelLinked = obj.primitiveOrNull("kernelLinked")?.toBooleanStrictOrNull(),
    )
}

// ---------------------------------------------------------------------------
// 统计页（proxies.status + status + traffic + connections）
// ---------------------------------------------------------------------------

/** 统计页状态。 */
sealed interface StatsUiState {
    data object Loading : StatsUiState

    /** 主数据（proxies.status）不可得：守护进程不可达或应答无法解读。 */
    data class Unavailable(val error: SyfuseError) : StatsUiState

    /**
     * @param daemon 守护进程 `status` 的展示子集；读取失败时为 null（附 [daemonError]）。
     * @param daemonError 守护进程信息读取失败的原因；成功时为 null。
     * @param traffic 累计流量（`traffic` 应答）；读取失败时为 null。
     * @param connectionCount 活动连接数（`connections` 应答）；读取失败时为 null。
     */
    data class Ready(
        val proxy: ProxyStatus,
        val daemon: DaemonInfo?,
        val daemonError: SyfuseError?,
        val traffic: TrafficTotals? = null,
        val connectionCount: Int? = null,
    ) : StatsUiState
}

/**
 * `traffic` 应答的展示子集（Go 侧 `conn.Stats` 直接序列化，是**裸对象**，
 * 没有 records 外壳 —— 见 `handleTraffic`）。
 */
data class TrafficTotals(
    val totalBytesIn: Long,
    val totalBytesOut: Long,
    val activeConnections: Int?,
    val totalConnections: Int?,
)

/** `proxies.status` 全量展示字段。任一字段都可以是"没上报"。 */
data class ProxyStatus(
    val state: String?,
    val running: Boolean?,
    val profileId: String?,
    val selected: String?,
    val nodeCount: Int?,
    val uptimeSec: Long?,
    val kernelLinked: Boolean?,
    val socksAddr: String?,
)

/** 守护进程 `status` 的展示子集。 */
data class DaemonInfo(
    val daemonVersion: String?,
    val protocolVersion: Int?,
    val uptimeSeconds: Long?,
)

internal suspend fun loadStats(client: DaemonClient): StatsUiState {
    val proxyResult = client.call("proxies.status")
    val proxy = when (proxyResult) {
        is SyfuseResult.Success -> parseProxyStatus(proxyResult.value)
        is SyfuseResult.Failure -> return StatsUiState.Unavailable(proxyResult.error)
    }
    if (proxy == null) {
        return StatsUiState.Unavailable(
            SyfuseError(
                code = ErrorCode.ENGINE_IPC_FAILED,
                technical = "proxies.status 返回非预期格式",
                userMessage = "守护进程返回了无法解读的代理状态",
                hint = "App 与模块版本可能不匹配",
            ),
        )
    }
    // 守护进程信息是附加段：读不到不影响主数据展示，如实降级。
    val daemonResult = client.call("status")
    val daemon = (daemonResult as? SyfuseResult.Success)?.let { parseDaemonInfo(it.value) }
    val daemonError = (daemonResult as? SyfuseResult.Failure)?.error

    // 流量与连接数同样是附加段：各自独立读取，一个失败不拖垮另一个，
    // 也都不影响 proxies.status 已经拿到的内容。
    val traffic = (client.call("traffic") as? SyfuseResult.Success)
        ?.let { parseTrafficTotals(it.value) }
    val connectionCount = parseConnectionCount(client.call("connections"))

    return StatsUiState.Ready(
        proxy = proxy,
        daemon = daemon,
        daemonError = daemonError,
        traffic = traffic,
        connectionCount = connectionCount,
    )
}

internal fun parseTrafficTotals(element: JsonElement): TrafficTotals? {
    val obj = element as? JsonObject ?: return null
    val inBytes = obj.primitiveOrNull("totalBytesIn")?.toLongOrNull() ?: return null
    val outBytes = obj.primitiveOrNull("totalBytesOut")?.toLongOrNull() ?: return null
    return TrafficTotals(
        totalBytesIn = inBytes,
        totalBytesOut = outBytes,
        activeConnections = obj.primitiveOrNull("activeConnections")?.toIntOrNull(),
        totalConnections = obj.primitiveOrNull("totalConnections")?.toIntOrNull(),
    )
}

/**
 * 从 `connections` 应答里数出活动连接数。
 *
 * 计数必须按 `endTime` 判活动（Go 的零值 time 序列化为
 * "0001-01-01T00:00:00Z"），而不是把 records 长度当活动数 ——
 * 后者含已关闭连接，会越用越高。
 * 读取失败返回 null（区别于"真的 0 条"）。
 */
internal fun parseConnectionCount(result: SyfuseResult<JsonElement>): Int? {
    val obj = (result as? SyfuseResult.Success)?.value as? JsonObject ?: return null
    val records = obj["records"] as? JsonArray ?: return null
    return records.count { el ->
        val time = ((el as? JsonObject)?.get("endTime") as? JsonPrimitive)
            ?.takeIf { it !is JsonNull }
            ?.content
        time == null || time.startsWith("0001-01-01")
    }
}

/**
 * 解析 `proxies.status`。字段与 Go 侧 `handleProxiesStatus` 一一对应；
 * 缺失即 null，**不补默认值**（`socksAddr` 在未监听时守护进程本就不上报）。
 *
 * `running` / `kernelLinked` 是 Go **布尔**字段，同样必须走
 * [primitiveOrNull]（见其注释）。
 */
internal fun parseProxyStatus(element: JsonElement): ProxyStatus? {
    val obj = element as? JsonObject ?: return null
    return ProxyStatus(
        state = obj.stringOrNull("state"),
        running = obj.primitiveOrNull("running")?.toBooleanStrictOrNull(),
        profileId = obj.stringOrNull("profileId"),
        selected = obj.stringOrNull("selected"),
        nodeCount = obj.primitiveOrNull("nodeCount")?.toIntOrNull(),
        uptimeSec = obj.primitiveOrNull("uptimeSec")?.toLongOrNull(),
        kernelLinked = obj.primitiveOrNull("kernelLinked")?.toBooleanStrictOrNull(),
        socksAddr = obj.stringOrNull("socksAddr"),
    )
}

/** 解析 `status`。只取展示需要的三个字段；其余（methods 等）不展示。 */
internal fun parseDaemonInfo(element: JsonElement): DaemonInfo? {
    val obj = element as? JsonObject ?: return null
    return DaemonInfo(
        daemonVersion = obj.stringOrNull("daemonVersion"),
        protocolVersion = obj.primitiveOrNull("protocolVersion")?.toIntOrNull(),
        uptimeSeconds = obj.primitiveOrNull("uptimeSeconds")?.toLongOrNull(),
    )
}

// ---------------------------------------------------------------------------
// 日志页（log.tail）
// ---------------------------------------------------------------------------

/** 日志页状态。三态显式分开。 */
sealed interface LogsUiState {
    data object Loading : LogsUiState

    data class Unavailable(val error: SyfuseError) : LogsUiState

    /**
     * @param lines 守护进程如实返回的日志行；可能为空。
     * @param note 守护进程自查到的读取问题（如日志文件不存在）。为空表示没有额外说明。
     */
    data class Ready(val lines: List<String>, val note: String?) : LogsUiState
}

/** 单次读取的日志行数（守护进程 `log.tail` 的 lines 参数）。 */
internal const val LOG_TAIL_LINES = 200

internal suspend fun loadLogTail(client: DaemonClient): LogsUiState =
    when (val result = client.tailLog(LOG_TAIL_LINES)) {
        is SyfuseResult.Success -> {
            val obj = result.value as? JsonObject
            val lines = (obj?.get("lines") as? JsonArray)
                ?.mapNotNull { (it as? JsonPrimitive)?.takeIf { p -> p.isString }?.content }
                ?: emptyList()
            val note = obj?.stringOrNull("note")?.takeIf { it.isNotBlank() }
            LogsUiState.Ready(lines = lines, note = note)
        }

        is SyfuseResult.Failure -> LogsUiState.Unavailable(result.error)
    }

