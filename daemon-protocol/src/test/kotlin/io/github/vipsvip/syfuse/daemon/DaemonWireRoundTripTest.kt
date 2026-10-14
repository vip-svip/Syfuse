package io.github.vipsvip.syfuse.daemon

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.jupiter.api.AfterAll
import org.junit.jupiter.api.BeforeAll
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.TestInstance
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * **真实往返测试**：Kotlin 客户端 ↔ 真实 syfused 进程 ↔ 真实 UDS。
 *
 * 这是 M3「内核打通」在沙箱里能得到的最强证据。它证明的是：
 * **两侧的手工实现真的能对上** —— JSON 字段名、协议版本、令牌校验、
 * 方法名、错误码、规则语义，任何一处漂移都会红。
 *
 * 为什么这件事以前做不到：真机传输用 `android.net.LocalSocket`（框架类，
 * JVM 单测里会抛 "not mocked"），而 `android.jar` 里又没有
 * `java.net.UnixDomainSocketAddress`（实测 android-35 无此类），
 * 于是这条往返连**编译**都过不去，只能等真机。
 * 把协议层搬进纯 JVM 的 `:daemon-protocol`、把传输抽成 [DaemonTransport] 之后，
 * 测试可以注入基于 JDK AF_UNIX 的实现（[JvmUnixSocketTransport]），
 * 用**本机真实编译出的 syfused 进程**跑通往返。
 *
 * 环境前提（已实测，非假设）：Go 1.23 与 JDK 21 在 Windows 10 1803+ 上
 * 都支持 AF_UNIX。`probeUnixSocket` 那段探测在 Go 侧测试里已有先例。
 *
 * **诚实边界（不得省略）**：
 *  1. 本测试**不能**证明 `android.net.LocalSocket` 在真机上可用 ——
 *     那一步仍然必须真机验证（见 PENDING-EXTERNAL.md）。
 *     它证明的是"协议两侧一致"，不是"平台 API 可用"。这两件事被刻意解耦。
 *  2. `proxies.start` 已实现（守护进程侧建真隧道），因此这里断言的是
 *     **真实启动**：应答带 state=running 与 socksAddr，且 stop 之后
 *     state=stopped。把"能如实失败"当成"隧道可用"是绝对不允许的；
 *     反过来，把"看起来启动成功"当成"流量真的在跑"同样不允许 ——
 *     字节是否真的过隧道由 Go 侧 `syfuse` 包的端到端用例守着。
 *  3. 本测试用的是本机 `go build` 出的宿主平台二进制，不是 Android 上的
 *     arm64 产物。交叉编译产物能否在设备上跑，属真机验证范围。
 *
 * 为什么缺 Go 工具链时**失败而不是跳过**：内核是本仓库的交付主体之一，
 * 工具链缺失属于"环境缺失"，按仓库约定（`tests/env.sh`）它**不得**被当成通过。
 * 一个被跳过的往返测试比没有这个测试更危险 —— 它会给人
 * "往返已经受保护了"的错觉。
 */
@TestInstance(TestInstance.Lifecycle.PER_CLASS)
class DaemonWireRoundTripTest {

    private lateinit var dataDir: File
    private lateinit var process: Process
    private lateinit var output: StringBuilder
    private lateinit var client: DaemonClient

    @BeforeAll
    fun startRealDaemon() {
        val repoRoot = findRepoRoot()
        val go = findGo()
        dataDir = shortTempDir()
        val binary = File(dataDir, exeName("syfused"))

        buildDaemon(repoRoot, go, binary)
        process = launchDaemon(binary, dataDir)

        val socketPath = File(dataDir, DaemonProtocol.SOCKET_NAME)
        val tokenPath = File(dataDir, DaemonProtocol.TOKEN_NAME)
        awaitFile(socketPath, 30_000, "socket")
        awaitFile(tokenPath, 30_000, "token")

        client = DaemonClient(
            dataDir = dataDir.absolutePath,
            tokenProvider = { tokenPath.readText().trim() },
            transport = JvmUnixSocketTransport(),
        )
    }

    @AfterAll
    fun stopRealDaemon() {
        if (::process.isInitialized) {
            process.destroy()
            if (!process.waitFor(5, TimeUnit.SECONDS)) process.destroyForcibly()
        }
        if (::dataDir.isInitialized) {
            // Windows 上 AF_UNIX 的 socket 文件在被硬杀（destroy 走的是
            // TerminateProcess，不是优雅退出）后可能残留且**删不掉**
            // （实测 "拒绝访问"）。这里只做尽力清理，绝不因为清理失败而让测试红 ——
            // 每次运行用的都是新随机目录，残留不会互相干扰。
            runCatching { dataDir.deleteRecursively() }
        }
    }

    // ------------------------------------------------------------------ 往返

    @Test
    fun `status 往返拿到真实守护进程的状态`() = runBlocking {
        val obj = ok(client.call("status"))

        assertThat(obj["daemonRunning"]!!.jsonPrimitive.content).isEqualTo("true")
        // 协议版本必须两边一致 —— 这是"手工实现的两侧对上了"的最直接证据
        assertThat(obj["protocolVersion"]!!.jsonPrimitive.content)
            .isEqualTo(DaemonProtocol.PROTOCOL_VERSION.toString())
        // 守护进程报出的方法清单里必须有规则方法（否则 App 会以为不可用）
        val methods = obj["methods"].toString()
        assertThat(methods).contains("rules.test")
        assertThat(methods).contains("status")
    }

    @Test
    fun `version 往返拿到守护进程版本与内核版本`() = runBlocking {
        val obj = ok(client.call("version"))

        assertThat(obj["protocol"]!!.jsonPrimitive.content)
            .isEqualTo(DaemonProtocol.PROTOCOL_VERSION.toString())
        assertThat(obj["daemon"]!!.jsonPrimitive.content).isNotEmpty()
        assertThat(obj["ruleEngine"]!!.jsonPrimitive.content).isNotEmpty()
    }

    /**
     * 跨语言语义一致性：同一组内联规则，守护进程（Go）给出的决策
     * 必须与 `core:rule`（Kotlin）一致。期望值与 `tests/expect.tsv` 同源。
     *
     * 这是"两个语言的实现没有漂移"在**真实进程**上的证据 ——
     * 而不仅仅是同一份语料在两个单测里各自跑。
     */
    @Test
    fun `rules_test 内联规则集给出与 Kotlin 侧一致的决策与优先级`() = runBlocking {
        val cases = listOf(
            Triple("block-a.test", "block", "normal-block"),
            Triple("allow-a.test", "allow", "normal-allow"),
            // 重要放行 > 重要拦截：同一 host 同时有 $important 的拦截与放行时，放行胜出
            Triple("imp.test", "allow", "important-allow"),
            Triple("nothing.test", "none", "none"),
        )
        for ((host, decision, priority) in cases) {
            val obj = ok(
                client.call(
                    "rules.test",
                    buildJsonObject {
                        put("rules", INLINE_RULES)
                        put("host", host)
                        put("collectChain", false)
                    },
                ),
            )
            assertThat(obj["decision"]!!.jsonPrimitive.content).isEqualTo(decision)
            assertThat(obj["priority"]!!.jsonPrimitive.content).isEqualTo(priority)
        }
    }

    @Test
    fun `rules_test 回传的统计证明守护进程真的编译了内联规则集`() = runBlocking {
        val obj = ok(
            client.call(
                "rules.test",
                buildJsonObject {
                    put("rules", INLINE_RULES)
                    put("host", "block-a.test")
                    put("collectChain", false)
                },
            ),
        )
        val stats = obj["stats"]!!.jsonObject
        // 4 条规则全部被索引（3 条域名锚定 + 1 条…… 实际是 4 条都可索引）
        assertThat(stats["rules"]!!.jsonPrimitive.content).isEqualTo("4")
        assertThat(stats["networkRules"]!!.jsonPrimitive.content).isEqualTo("4")
        assertThat(stats["errors"]!!.jsonPrimitive.content).isEqualTo("0")
    }

    @Test
    fun `collectChain 为真时链路里能看到命中规则与是否生效`() = runBlocking {
        val obj = ok(
            client.call(
                "rules.test",
                buildJsonObject {
                    put("rules", INLINE_RULES)
                    put("host", "block-a.test")
                    put("resourceType", "script")
                    put("collectChain", true)
                },
            ),
        )
        val chain = obj["chain"]!!.jsonArray
        assertThat(chain).isNotEmpty()
        val first = chain[0].jsonObject
        assertThat(first["raw"]!!.jsonPrimitive.content).isEqualTo("||block-a.test^")
        assertThat(first["lineNo"]!!.jsonPrimitive.content).isEqualTo("1")
        assertThat(first["applied"]!!.jsonPrimitive.content).isEqualTo("true")
        assertThat(first["sourceId"]!!.jsonPrimitive.content).isEqualTo("inline")
    }

    @Test
    fun `rules_test 能从 url 推导 host 再裁决`() = runBlocking {
        val obj = ok(
            client.call(
                "rules.test",
                buildJsonObject {
                    put("rules", "||path-a.test/ads/")
                    put("url", "https://path-a.test/ads/x.js")
                    put("resourceType", "script")
                },
            ),
        )
        assertThat(obj["decision"]!!.jsonPrimitive.content).isEqualTo("block")
        assertThat(obj["matched"]!!.jsonObject["raw"]!!.jsonPrimitive.content)
            .isEqualTo("||path-a.test/ads/")
    }

    @Test
    fun `rules_syntax 回传带行号的逐条诊断`() = runBlocking {
        // 第 2 行是一个错误的 $badfilter（缺匹配模式）。诊断必须带行号，
        // 否则"规则体检"界面无法把问题定位到用户能改的那一行。
        val obj = ok(
            client.call(
                "rules.syntax",
                buildJsonObject { put("rules", "||good.test^\n\$badfilter=zzz") },
            ),
        )
        val diagnostics = obj["diagnostics"]!!.jsonArray
        assertThat(diagnostics).isNotEmpty()
        val first = diagnostics[0].jsonObject
        assertThat(first["severity"]!!.jsonPrimitive.content).isEqualTo("error")
        assertThat(first["lineNo"]!!.jsonPrimitive.content).isEqualTo("2")
        assertThat(first["raw"]!!.jsonPrimitive.content).isEqualTo("\$badfilter=zzz")
        assertThat(first["message"]!!.jsonPrimitive.content).isNotEmpty()
        // stats 里必须把错误计数带出来（界面靠它决定是否显示红点）
        assertThat(obj["stats"]!!.jsonObject["errors"]!!.jsonPrimitive.content).isEqualTo("1")
    }

    @Test
    fun `rules_test 缺少 host 时如实报错并说明原因`() = runBlocking {
        val error = fail(
            client.call("rules.test", buildJsonObject { put("rules", "||a.test^") }),
        )
        assertThat(error.technical).contains("缺少 host")
    }

    @Test
    fun `不存在的资源类型被拒绝并列出可用值`() = runBlocking {
        val error = fail(
            client.call(
                "rules.test",
                buildJsonObject {
                    put("rules", INLINE_RULES)
                    put("host", "a.test")
                    put("resourceType", "not-a-real-type")
                },
            ),
        )
        assertThat(error.technical).contains("not-a-real-type")
        // 可用值必须列出来，否则用户不知道该填什么
        assertThat(error.technical).contains("script")
    }

    @Test
    fun `错误令牌被守护进程拒绝且 App 给出可操作提示`() = runBlocking {
        val badClient = DaemonClient(
            dataDir = dataDir.absolutePath,
            tokenProvider = { "definitely-not-the-right-token" },
            transport = JvmUnixSocketTransport(),
        )

        val error = fail(badClient.call("status"))
        // 令牌错是**正常状态**（未配对），不是异常：必须有面向用户的话，
        // 且不能把 "unauthorized" 这种协议黑话直接甩给用户。
        assertThat(error.userMessage).isNotEmpty()
        assertThat(error.userMessage).doesNotContain("unauthorized")
        assertThat(error.userMessage).contains("令牌")
        assertThat(error.technical).contains(DaemonErrorCode.UNAUTHORIZED)
    }


    // 用例名里**不能出现双引号**：Kotlin 会用反引号里的名字生成 .class 文件名，
    // 而 Windows 的文件名不允许 " * ? < > | : —— 会直接炸成
    // "InvalidPathException: Illegal char <"> 这种看不出跟命名有关的内部编译器错误。
    // 这里用中文书名号，既表达了"引用"的意思，又不触发文件名限制。
    @Test
    fun `proxies_start 真的建立隧道并报回监听地址`() = runBlocking {
        // UdsDataPlane.start 转发到 proxies.start；后者驱动真实引擎。
        // 直连模式：不需要节点，守护进程内就能起一条 SOCKS5 入站隧道。
        //
        // configJson 用**正确**的字段名（守护进程侧 proxyStartParams.ConfigJSON）。
        // 历史上这里写的是 coreConfig，于是参数被当成没传，守护进程转而去读
        // 磁盘上的 syfuse.conf，测试环境里没有那个文件 —— 一个字段名拼错
        // 把"启动隧道"测成了"读配置失败"。
        val started = ok(
            client.call(
                "proxies.start",
                buildJsonObject {
                    put("profileId", "p")
                    put("configJson", """{"schemaVersion":1,"routingMode":"direct"}""")
                },
            ),
        )
        assertThat(started["state"]?.jsonPrimitive?.content).isEqualTo("running")
        assertThat(started["running"]?.jsonPrimitive?.content).isEqualTo("true")
        val socksAddr = started["socksAddr"]?.jsonPrimitive?.content
        assertThat(socksAddr).isNotEmpty()
        // 端口可能是系统分配的：能用它连上才是真的在听。连不上会抛异常，
        // 测试直接红 —— 不留"看起来返回了地址"这种半真半假的结论。
        java.net.Socket().use {
            it.connect(java.net.InetSocketAddress("127.0.0.1", socksAddr!!.substringAfterLast(':').toInt()), 2000)
        }

        val stopped = ok(client.call("proxies.stop", null))
        assertThat(stopped["state"]?.jsonPrimitive?.content).isEqualTo("stopped")
        assertThat(stopped["running"]?.jsonPrimitive?.content).isEqualTo("false")
    }

    @Test
    fun `proxies_status_未启动时不谎报运行中`() = runBlocking {
        // start 之前（或 stop 之后）必须是 stopped/running=false，
        // 且没有 socksAddr —— 否则 UI 会显示"已连接"却无处送流量。
        val st = ok(client.call("proxies.status", null))
        assertThat(st["running"]?.jsonPrimitive?.content).isEqualTo("false")
        assertThat(st["socksAddr"]).isNull()
    }

    @Test
    fun `完全未知的方法才返回 unknown_method`() = runBlocking {
        val error = fail(client.call("definitely.not.a.method"))
        assertThat(error.technical).contains(DaemonErrorCode.UNKNOWN_METHOD)
    }

    // ------------------------------------------------------------------ 工具

    private fun ok(result: SyfuseResult<JsonElement>) = result.let {
        assertThat(it).isInstanceOf(SyfuseResult.Success::class.java)
        (it as SyfuseResult.Success).value.jsonObject
    }

    private fun fail(result: SyfuseResult<JsonElement>) = result.let {
        assertThat(it).isInstanceOf(SyfuseResult.Failure::class.java)
        (it as SyfuseResult.Failure).error
    }

    private fun buildDaemon(repoRoot: File, go: File, binary: File) {
        val srcDir = File(repoRoot, "src")
        val cacheDir = File(System.getProperty("java.io.tmpdir"), "syfuse-gocache")
        val proc = ProcessBuilder(
            go.absolutePath, "build", "-o", binary.absolutePath, "./cmd/syfused",
        )
            .directory(srcDir)
            .redirectErrorStream(true)
            .apply {
                environment()["GOFLAGS"] = "-mod=mod"
                environment()["GOPROXY"] = "off"
                environment()["GOCACHE"] = cacheDir.absolutePath
                environment()["CGO_ENABLED"] = "0"
            }
            .start()
        val log = proc.inputStream.bufferedReader().readText()
        val code = proc.waitFor()
        if (code != 0 || !binary.isFile) {
            throw AssertionError("syfused 构建失败（exit=$code）。输出：\n$log")
        }
    }

    private fun launchDaemon(binary: File, dir: File): Process {
        val proc = ProcessBuilder(binary.absolutePath, "daemon")
            .directory(dir)
            .redirectErrorStream(true)
            .apply { environment()["SYFUSE_DATA_DIR"] = dir.absolutePath }
            .start()
        output = StringBuilder()
        // 持续消费输出：不读的话管道写满会让守护进程阻塞在写日志上。
        Thread {
            runCatching {
                proc.inputStream.bufferedReader().forEachLine { line ->
                    synchronized(output) { output.appendLine(line) }
                }
            }
        }.apply { isDaemon = true }.start()
        return proc
    }

    private fun awaitFile(file: File, timeoutMs: Long, what: String) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (file.exists()) return
            if (!process.isAlive) {
                throw AssertionError(
                    "守护进程在 $what 出现前就退出了（exit=${process.exitValue()}）。" +
                        "日志：\n${daemonLog()}输出：\n$output",
                )
            }
            Thread.sleep(50)
        }
        throw AssertionError(
            "等待 $what 超时（${file.absolutePath}）。守护进程日志：\n${daemonLog()}输出：\n$output",
        )
    }

    /**
     * 读守护进程自己的日志文件。
     *
     * 必须带上它：`syfused` 把日志写进 `<data>/log/syfused.log` 而**不是** stdout，
     * 所以"启动失败"时 stdout 是空的 —— 只看 stdout 会得到
     * "守护进程在 socket 出现前就退出了" 这种毫无信息量的报错。
     * 实测就是这么被坑过一次（真正原因是 bind 失败：陈旧 socket 文件占用地址）。
     */
    private fun daemonLog(): String {
        val f = File(dataDir, "log/syfused.log")
        return if (f.isFile) f.readText() else "<无日志文件>\n"
    }

    private fun findRepoRoot(): File {
        var dir: File? = File(".").absoluteFile
        while (dir != null) {
            if (File(dir, "settings.gradle.kts").isFile && File(dir, "src/go.mod").isFile) return dir
            dir = dir.parentFile
        }
        throw AssertionError("未找到仓库根目录（需要同时存在 settings.gradle.kts 与 src/go.mod）")
    }

    /**
     * 定位 Go 工具链。
     *
     * 找不到时**抛错**而不是跳过：内核是本仓库的交付主体，工具链缺失是
     * "环境缺失"，按仓库约定不得被当成通过。
     */
    private fun findGo(): File {
        val candidates = buildList {
            System.getenv("SYFUSE_GO")?.takeIf { it.isNotBlank() }?.let { add(File(it)) }
            val home = System.getProperty("user.home")
            add(File(home, ".workbuddy-ai/toolchain/go/bin/${exeName("go")}"))
            add(File("/usr/local/go/bin/go"))
            add(File("/usr/bin/go"))
        }
        candidates.firstOrNull { it.isFile }?.let { return it }

        // 退一步：PATH 上找
        val fromPath = System.getenv("PATH").orEmpty()
            .split(File.pathSeparator)
            .map { File(it, exeName("go")) }
            .firstOrNull { it.isFile }
        return fromPath ?: throw AssertionError(
            "找不到 Go 工具链，无法验证真实往返。已尝试：\n" +
                candidates.joinToString("\n") { "  - ${it.absolutePath}" } +
                "\n可设置 SYFUSE_GO 指向 go 可执行文件。" +
                "注意：本测试**不跳过**，因为内核是交付主体，环境缺失不得算通过。",
        )
    }

    private fun exeName(base: String): String =
        if (System.getProperty("os.name").lowercase().contains("win")) "$base.exe" else base

    /**
     * 建一个**短路径**临时目录。
     *
     * 原因：AF_UNIX 的 `sockaddr_un.sun_path` 只有约 108 字节，
     * 把 socket 放在深层临时目录里会直接 bind 失败（表现为
     * "invalid argument"，与权限无关，极难排查）。
     */
    private fun shortTempDir(): File {
        val base = File(System.getProperty("java.io.tmpdir"))
        val dir = File(base, "syf-" + java.util.UUID.randomUUID().toString().take(8))
        if (!dir.mkdirs()) throw AssertionError("无法创建临时目录 ${dir.absolutePath}")
        return dir
    }

    private companion object {
        /**
         * 与 `tests/expect.tsv` 同源的内联规则集。
         *
         * `$important` 在 Kotlin 里必须转义成 `\$important`，否则会被当成
         * 字符串模板引用变量 —— 这里曾经因此编译失败过，注释留作警示。
         */
        const val INLINE_RULES = "||block-a.test^\n@@||allow-a.test^\n||imp.test^\$important\n@@||imp.test^\$important"
    }
}
