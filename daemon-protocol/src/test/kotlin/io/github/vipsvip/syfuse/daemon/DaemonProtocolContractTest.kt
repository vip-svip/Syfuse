package io.github.vipsvip.syfuse.daemon

import com.google.common.truth.Truth.assertThat
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 跨语言协议一致性测试（Kotlin ↔ Go）。
 *
 * **为什么必须有这个测试**
 *
 * [DaemonProtocol] 与 `src/internal/ipc/ipc.go` 是同一份协议的**两侧手工实现**。
 * 文件头写着"必须同步修改"，但在此之前**没有任何东西在强制这件事** ——
 * 漂移的后果是静默的：字段名改了、错误码改了、方法名改了，
 * 编译期谁都不会报错，只有在真机上控制通道整个失灵时才会暴露，
 * 而那正是最贵、最难排查的时刻（App 与模块是两个独立升级的产物）。
 *
 * 本测试把 Go 源码当作**唯一事实来源**，逐项核对 Kotlin 侧是否与之对齐：
 *
 *  1. `PROTOCOL_VERSION`
 *  2. `Request` / `Response` 的 JSON 字段名 —— 用 Kotlin **真实序列化器**产出字段集合，
 *     而不是手写 JSON 字符串（后者会掩盖 `@SerialName` 写错）
 *  3. 错误码集合（从 Go 的 dispatch 里抓 `Code: "..."` 字面量）
 *  4. 已实现方法集合 / 待实现方法 → 里程碑映射（从 Go 的 `Handle` 注册处抓）
 *  5. 数据目录 / socket / token / pid / 清单路径
 *
 * **设计取舍：不用 golden 文件。**
 * "Go 生成 golden → Kotlin 读 golden" 的流程有**陈旧**风险：改了 Go 却忘了重新生成，
 * 测试照样是绿的。直接读源码不存在这个问题 —— Go 源码一改，本测试立刻看到。
 * 代价是提取依赖正则；因此约定：**提取不到一律判失败**，绝不静默通过。
 * 若 Go 侧重构导致提取失效，本测试会红着告诉你去更新提取方式，
 * 而不是悄悄退化成"什么都不检查"。
 */
class DaemonProtocolContractTest {

    private val repoRoot: File = findRepoRoot()
    private val ipcGo: String = File(repoRoot, "src/internal/ipc/ipc.go").readText()
    private val mainGo: String = File(repoRoot, "src/cmd/syfused/main.go").readText()

    // ------------------------------------------------------------------
    // 提取工具
    // ------------------------------------------------------------------

    private fun fail(msg: String): Nothing = throw AssertionError(msg)

    private fun extractOne(src: String, regex: Regex, what: String): String =
        regex.find(src)?.groupValues?.get(1) ?: fail(
            "无法从 Go 源码提取 $what（正则 `${regex.pattern}` 未匹配）。" +
                "若是 Go 侧重构导致，请同步更新本测试的提取方式 —— " +
                "不要让本测试静默失去覆盖。",
        )

    private fun extractAll(src: String, regex: Regex): List<String> =
        regex.findAll(src).map { it.groupValues[1] }.toList()

    /** 抓取 `type Name struct { ... }` 的字段体。 */
    private fun structBody(src: String, name: String): String = extractOne(
        src,
        Regex("type\\s+$name\\s+struct\\s*\\{(.*?)\\n\\}", RegexOption.DOT_MATCHES_ALL),
        "结构体 $name",
    )

    /** 从结构体定义里取出 json 标签名（跳过 `json:"-"`）。 */
    private fun jsonFields(src: String, structName: String): Set<String> =
        Regex("json:\"([^\",]+)")
            .findAll(structBody(src, structName))
            .map { it.groupValues[1] }
            .filter { it != "-" }
            .toSet()

    private fun goErrorCodes(): Set<String> =
        extractAll(ipcGo, Regex("Code:\\s*\"([a-z_]+)\"")).toSet()

    private fun goImplementedMethods(): Set<String> =
        extractAll(mainGo, Regex("srv\\.Handle\\(\"([a-zA-Z.]+)\"")).toSet()

    private fun goPendingMethods(): Map<String, String> =
        // 注意字符类里必须有 `M`：里程碑可以写成 "M3/M4"，第二段的 M 也得匹配。
        // 漏掉它会让 "proxies" 与 "rules.test" 静默漏抓 —— 这正是本测试存在的意义。
        Regex("\\{\"([a-zA-Z.]+)\",\\s*\"(M[0-9M/]+)\"\\}")
            .findAll(mainGo)
            .associate { it.groupValues[1] to it.groupValues[2] }

    private fun kotlinFieldNames(element: kotlinx.serialization.json.JsonElement): Set<String> =
        (element as JsonObject).keys

    // ------------------------------------------------------------------
    // 1. 协议版本
    // ------------------------------------------------------------------

    @Test
    fun `协议版本与 Go 侧一致`() {
        val go = extractOne(ipcGo, Regex("const\\s+ProtocolVersion\\s*=\\s*(\\d+)"), "ProtocolVersion")
        assertThat(DaemonProtocol.PROTOCOL_VERSION).isEqualTo(go.toInt())
    }

    // ------------------------------------------------------------------
    // 2. JSON 字段名（用真实序列化器，不手写 JSON）
    // ------------------------------------------------------------------

    @Test
    fun `Request 的 JSON 字段名与 Go 侧一致`() {
        val goFields = jsonFields(ipcGo, "Request")
        assertThat(goFields).isNotEmpty()

        // 每个字段都填非空值：explicitNulls=false 会丢掉 null 字段，
        // 那样就测不出"字段被整个漏掉"这类漂移。
        val kotlinFields = kotlinFieldNames(
            DaemonProtocol.json.encodeToJsonElement(
                DaemonRequest.serializer(),
                DaemonRequest(id = "1", authorization = "tok", method = "status", params = JsonPrimitive("x")),
            ),
        )
        assertThat(kotlinFields).isEqualTo(goFields)
    }

    @Test
    fun `Response 的 JSON 字段名与 Go 侧一致`() {
        val goFields = jsonFields(ipcGo, "Response")
        assertThat(goFields).isNotEmpty()

        val kotlinFields = kotlinFieldNames(
            DaemonProtocol.json.encodeToJsonElement(
                DaemonResponse.serializer(),
                DaemonResponse(id = "1", ok = true, error = "e", code = "c", result = JsonPrimitive(1)),
            ),
        )
        assertThat(kotlinFields).isEqualTo(goFields)
    }

    // ------------------------------------------------------------------
    // 3. 错误码
    // ------------------------------------------------------------------

    @Test
    fun `错误码集合与 Go 侧一致`() {
        val go = goErrorCodes()
        assertThat(go).isNotEmpty()

        val kotlinCodes = setOf(
            DaemonErrorCode.BAD_REQUEST,
            DaemonErrorCode.UNAUTHORIZED,
            DaemonErrorCode.VERSION_TOO_NEW,
            DaemonErrorCode.UNKNOWN_METHOD,
            DaemonErrorCode.HANDLER_ERROR,
            DaemonErrorCode.ENCODE_ERROR,
        )
        assertThat(kotlinCodes).isEqualTo(go)
    }

    @Test
    fun `协议级错误码必须有本地翻译，内部错误码有意透传原文`() {
        // 协议级错误：守护进程给的是机器语（"unauthorized"），对用户毫无意义，
        // App 必须自己翻译成可操作的话。
        assertThat(daemonErrorHint(DaemonErrorCode.UNAUTHORIZED, "RAW")).isNotEqualTo("RAW")
        assertThat(daemonErrorHint(DaemonErrorCode.VERSION_TOO_NEW, "RAW")).isNotEqualTo("RAW")
        assertThat(daemonErrorHint(DaemonErrorCode.UNKNOWN_METHOD, "RAW")).isNotEqualTo("RAW")
        assertThat(daemonErrorHint(DaemonErrorCode.BAD_REQUEST, "RAW")).isNotEqualTo("RAW")

        // 内部错误（handler_error / encode_error）：守护进程自己写的就是中文说明
        // （例如"方法 \"proxies\" 尚未实现（计划里程碑 M3/M4）"），比 App 能编的任何话都准确。
        // 这里透传是**有意为之**，钉住它是为了将来有人"顺手补一个 else"时能看到原因。
        assertThat(daemonErrorHint(DaemonErrorCode.HANDLER_ERROR, "守护进程给的说明"))
            .isEqualTo("守护进程给的说明")
    }

    // ------------------------------------------------------------------
    // 4. 方法清单
    // ------------------------------------------------------------------

    @Test
    fun `已实现方法集合与 Go 侧注册的处理器一致`() {
        val go = goImplementedMethods()
        assertThat(go).isNotEmpty()
        assertThat(DaemonProtocol.IMPLEMENTED_METHODS).isEqualTo(go)
    }

    @Test
    fun `待实现方法与里程碑映射与 Go 侧一致`() {
        val go = goPendingMethods()
        // M14：全部 UDS 方法已实现，pending 表两侧都必须清空。
        // 若未来新增未实现方法，此表恢复非空——两侧仍须一致。
        assertThat(go).isEmpty()
        assertThat(DaemonProtocol.PENDING_METHODS).isEqualTo(go)
    }

    @Test
    fun `待实现方法表没有被正则漏抓`() {
        // 上一条测试只保证"提取出来的"与 Kotlin 一致。
        // 如果正则漏抓了某一条，而 Kotlin 侧恰好也没写它，两边仍然相等 —— 假绿。
        // 这里**独立**数一遍 Go 源码里 `{"名字",` 的形态，确保一条都没漏。
        // （`{"rotated": true}` 这类 map 字面量因为名字后面跟的是 `:` 而不是 `,`，不会被算进来。）
        val declared = extractAll(mainGo, Regex("\\{\"([a-zA-Z.]+)\",")).toSet()
        // pending 表清零后两侧都必须是空集（若恢复非空仍须相等）。
        assertThat(goPendingMethods().keys).isEqualTo(declared)
    }

    @Test
    fun `已实现与待实现方法互斥`() {
        // 同一方法同时出现在两张表里，UI 的能力判定就会自相矛盾：
        // DaemonCapability.isImplemented 说"可用"，pendingMilestone 又说"计划 M9"。
        val overlap = DaemonProtocol.IMPLEMENTED_METHODS intersect DaemonProtocol.PENDING_METHODS.keys
        assertThat(overlap).isEmpty()
    }

    @Test
    fun `每个待实现方法都带计划里程碑`() {
        // 空里程碑会让 UI 显示"不可用（计划 ）"，等于没有给用户任何预期。
        val empty = DaemonProtocol.PENDING_METHODS.filterValues { it.isBlank() }
        assertThat(empty).isEmpty()
    }

    // ------------------------------------------------------------------
    // 5. 路径常量
    // ------------------------------------------------------------------

    @Test
    fun `数据目录与 Go 侧的 base 默认值一致`() {
        val go = extractOne(mainGo, Regex("base\\s*=\\s*\"([^\"]+)\""), "数据根目录 base")
        assertThat(DaemonProtocol.DATA_DIR).isEqualTo(go)
    }

    @Test
    fun `文件名常量在 Go 侧真实出现`() {
        // 这三个名字是 App 侧拼路径用的；Go 侧改了名字而 App 不知道，
        // 表现为"连不上守护进程"，且错误信息完全指不出原因。
        assertThat(mainGo).contains("\"${DaemonProtocol.SOCKET_NAME}\"")
        assertThat(mainGo).contains("\"${DaemonProtocol.TOKEN_NAME}\"")
        assertThat(mainGo).contains("\"${DaemonProtocol.PID_NAME}\"")
    }

    @Test
    fun `还原清单路径与 Go 侧构造表达式一致`() {
        // Go 侧是 filepath.Join(base, "manifest", "installed.json")。
        // 这里由 Kotlin 常量反推出该表达式再比对 —— 两侧任何一边改了都会红。
        val expected = DaemonProtocol.MANIFEST_REL_PATH
            .split("/")
            .joinToString(prefix = "filepath.Join(base, ", postfix = ")") { "\"$it\"" }
        assertThat(mainGo).contains(expected)
    }

    // ------------------------------------------------------------------
    // 6. 超时
    // ------------------------------------------------------------------

    @Test
    fun `超时为正且不超过十秒`() {
        // UDS 是本机通信。超时给太大（比如 60s）会让"守护没起来"表现为界面卡死；
        // 给太小又会在设备负载高时误判。钉住一个合理区间。
        assertThat(DaemonProtocol.TIMEOUT_MS).isGreaterThan(0L)
        assertThat(DaemonProtocol.TIMEOUT_MS).isAtMost(10_000L)
    }

    // ------------------------------------------------------------------
    // 定位仓库根目录
    // ------------------------------------------------------------------

    private companion object {
        /**
         * 从测试工作目录向上找到同时含 `settings.gradle.kts` 与
         * `src/internal/ipc/ipc.go` 的目录。
         *
         * Gradle 默认把模块目录（`daemon/`）设为测试工作目录，因此需要向上走一层。
         * 找不到就**抛错**：静默返回 null 会让整个契约测试悄悄失效，
         * 那比没有这个测试更糟 —— 它会给人"协议已经受保护了"的错觉。
         */
        fun findRepoRoot(): File {
            var dir: File? = File(".").absoluteFile
            while (dir != null) {
                val hasSettings = File(dir, "settings.gradle.kts").isFile
                val hasGoIpc = File(dir, "src/internal/ipc/ipc.go").isFile
                if (hasSettings && hasGoIpc) return dir
                dir = dir.parentFile
            }
            throw AssertionError(
                "未找到仓库根目录：需要同时存在 settings.gradle.kts 与 src/internal/ipc/ipc.go。" +
                    "当前工作目录=${File(".").absolutePath}",
            )
        }
    }
}
