package io.github.vipsvip.syfuse.engine

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.daemon.DaemonClient
import io.github.vipsvip.syfuse.daemon.DaemonProtocol
import io.github.vipsvip.syfuse.daemon.DaemonResponse
import io.github.vipsvip.syfuse.daemon.DaemonTransport
import io.github.vipsvip.syfuse.domain.engine.EngineConfig
import io.github.vipsvip.syfuse.domain.model.Profile
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Test

/**
 * 可控传输替身：按请求方法返回预置应答，并记录全部请求行。
 *
 * 用它而不是 mock 框架，是因为这里要验证的是**协议层的真实字节**：
 * 请求行是否是可解析的 JSON、字段名是否正确、id 是否回填。
 * mock 框架会把这一层也替换掉，反而测不到东西。
 */
private class FakeTransport(
    private val responder: (method: String, id: String) -> String,
) : DaemonTransport {
    val methods = mutableListOf<String>()

    override fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String? {
        val req = DaemonProtocol.json.parseToJsonElement(requestLine).jsonObject
        val method = req["method"]!!.jsonPrimitive.content
        val id = req["id"]!!.jsonPrimitive.content
        methods += method
        return responder(method, id)
    }
}

private fun okLine(id: String, result: JsonElement): String =
    DaemonProtocol.json.encodeToString(
        DaemonResponse.serializer(),
        DaemonResponse(id = id, ok = true, result = result),
    )

private fun errLine(id: String, code: String, error: String): String =
    DaemonProtocol.json.encodeToString(
        DaemonResponse.serializer(),
        DaemonResponse(id = id, ok = false, code = code, error = error),
    )

private fun json(text: String): JsonElement = DaemonProtocol.json.parseToJsonElement(text)

private fun planeWith(responder: (method: String, id: String) -> String): Pair<UdsDataPlane, FakeTransport> {
    val transport = FakeTransport(responder)
    val client = DaemonClient(
        dataDir = "/tmp/syfuse-test",
        tokenProvider = { "test-token" },
        transport = transport,
    )
    return UdsDataPlane(client) to transport
}

private fun config() = EngineConfig(profile = Profile(id = "p", name = "测试配置"), coreConfig = "{}")

class UdsDataPlaneTest {

    @Test
    fun `探测成功后才标记可用并记下守护进程版本`() = runTest {
        val (plane, transport) = planeWith { _, id ->
            okLine(id, json("""{"daemonRunning":true,"daemonVersion":"0.1.0"}"""))
        }

        // 探测之前：如实声明"还没通信过"，而不是假装可用
        assertThat(plane.available).isFalse()
        assertThat(plane.unavailableReason).contains("尚未与守护进程通信过")

        assertThat(plane.probeAvailability()).isTrue()
        assertThat(plane.available).isTrue()
        assertThat(plane.daemonVersion).isEqualTo("0.1.0")
        assertThat(plane.unavailableReason).isNull()
        // 探测必须用 status：它同时验证 socket 可连、令牌正确、协议匹配
        assertThat(transport.methods).containsExactly("status")
    }

    @Test
    fun `探测失败时原因可展示且不含技术黑话`() = runTest {
        val (plane, _) = planeWith { _, id ->
            errLine(id, "unauthorized", "令牌不匹配")
        }

        assertThat(plane.probeAvailability()).isFalse()
        assertThat(plane.available).isFalse()
        val reason = plane.unavailableReason
        assertThat(reason).isNotNull()
        // 用户看到的是"访问令牌不匹配…"，而不是 "unauthorized"
        assertThat(reason).contains("访问令牌")
        assertThat(reason).doesNotContain("unauthorized")
    }

    @Test
    fun `隧道未实现时 start 如实失败并把守护进程的原因带上来`() = runTest {
        val (plane, transport) = planeWith { method, id ->
            when (method) {
                "status" -> okLine(id, json("""{"daemonVersion":"0.1.0"}"""))
                else -> errLine(id, "handler_error", "方法 \"proxies.start\" 尚未实现（计划里程碑 M3）")
            }
        }
        plane.probeAvailability()

        val result = plane.start(config())

        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        // 原因必须来自守护进程，而不是 App 自己编的
        assertThat(error.technical).contains("proxies.start")
        assertThat(error.technical).contains("M3")
        assertThat(transport.methods).contains("proxies.start")
    }

    @Test
    fun `从未启动过隧道时 stop 是成功的空操作`() = runTest {
        val (plane, transport) = planeWith { _, id -> okLine(id, json("""{}""")) }

        val result = plane.stop()

        assertThat(result).isInstanceOf(SyfuseResult.Success::class.java)
        // 关键：不向守护进程发一次注定失败的 proxies.stop，也不把引擎推进 ERROR
        assertThat(transport.methods).isEmpty()
    }

    @Test
    fun `启动成功后再 stop 会真的通知守护进程`() = runTest {
        val (plane, transport) = planeWith { method, id ->
            if (method == "proxies.start") okLine(id, json("""{"started":true}"""))
            else okLine(id, json("""{"stopped":true}"""))
        }

        assertThat(plane.start(config())).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat(plane.stop()).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat(transport.methods).containsExactly("proxies.start", "proxies.stop").inOrder()

        // 停过一次之后，再停又是空操作
        transport.methods.clear()
        assertThat(plane.stop()).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat(transport.methods).isEmpty()
    }

    @Test
    fun `规则能力是真实可用的`() = runTest {
        val (plane, transport) = planeWith { method, id ->
            when (method) {
                "rules.stats" -> okLine(id, json("""{"stats":{"rules":27,"indexed":24,"linear":3}}"""))
                "rules.test" -> okLine(
                    id,
                    json("""{"decision":"block","priority":"normal-block","blocked":true,"chain":[]}"""),
                )
                else -> errLine(id, "handler_error", "未实现")
            }
        }

        val stats = plane.ruleSetStats()
        assertThat(stats).isInstanceOf(SyfuseResult.Success::class.java)
        val statsObj = (stats as SyfuseResult.Success).value["stats"]!!.jsonObject
        assertThat(statsObj["rules"]!!.jsonPrimitive.content).isEqualTo("27")

        val tested = plane.testRule(host = "block-a.test")
        assertThat(tested).isInstanceOf(SyfuseResult.Success::class.java)
        val obj = (tested as SyfuseResult.Success).value
        assertThat(obj["decision"]!!.jsonPrimitive.content).isEqualTo("block")

        // 规则测试必须要求链路：界面要靠它展示"哪条命中、哪条被跳过"
        assertThat(transport.methods).containsExactly("rules.stats", "rules.test").inOrder()
    }

    @Test
    fun `缺少令牌时给出可操作提示而不是抛异常`() = runTest {
        val client = DaemonClient(
            dataDir = "/tmp/syfuse-test",
            tokenProvider = { null },
            transport = FakeTransport { _, id -> okLine(id, json("""{}""")) },
        )
        val plane = UdsDataPlane(client)

        assertThat(plane.probeAvailability()).isFalse()
        assertThat(plane.unavailableReason).contains("令牌")
    }

    // 用例名里不能出现双引号：Windows 不允许文件名含 "，而 Kotlin 会用
    // 反引号里的名字生成 .class 文件名（详见 DaemonWireRoundTripTest 的同名注释）。
    @Test
    fun `守护进程不可达时错误码是 IPC 失败而不是「模块未安装」`() = runTest {
        val client = DaemonClient(
            dataDir = "/tmp/syfuse-test",
            tokenProvider = { "tok" },
            transport = object : DaemonTransport {
                override fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String? =
                    throw java.io.IOException("connect refused")
            },
        )
        val plane = UdsDataPlane(client)

        assertThat(plane.probeAvailability()).isFalse()
        val result = plane.start(config())
        assertThat((result as SyfuseResult.Failure).error.code).isEqualTo(ErrorCode.ENGINE_IPC_FAILED)
    }

    @Test
    fun `返回非对象时如实报错而不是崩溃`() = runTest {
        val (plane, _) = planeWith { method, id ->
            when (method) {
                "rules.stats" -> okLine(id, json(""""这是一个字符串，不是对象""""))
                else -> okLine(id, json("""{}"""))
            }
        }

        val result = plane.ruleSetStats()
        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        assertThat(error.code).isEqualTo(ErrorCode.ENGINE_IPC_FAILED)
        assertThat(error.userMessage).isNotEmpty()
    }

    @Test
    fun `空应答被识别为守护进程已退出`() = runTest {
        val client = DaemonClient(
            dataDir = "/tmp/syfuse-test",
            tokenProvider = { "tok" },
            transport = object : DaemonTransport {
                override fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String? = null
            },
        )
        val plane = UdsDataPlane(client)

        assertThat(plane.probeAvailability()).isFalse()
        assertThat(plane.unavailableReason).contains("没有返回内容")
    }

    @Test
    fun `请求行是可解析的 JSON 且带令牌`() = runTest {
        var seen: JsonObject? = null
        val transport = object : DaemonTransport {
            override fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String? {
                val obj = DaemonProtocol.json.parseToJsonElement(requestLine).jsonObject
                seen = obj
                return okLine(obj["id"]!!.jsonPrimitive.content, json("""{}"""))
            }
        }
        val plane = UdsDataPlane(DaemonClient("/tmp/syfuse-test", { "tok-123" }, transport))

        plane.probeAvailability()

        val req = seen!!
        assertThat(req["method"]!!.jsonPrimitive.content).isEqualTo("status")
        assertThat(req["authorization"]!!.jsonPrimitive.content).isEqualTo("tok-123")
        assertThat(req["protocolVersion"]!!.jsonPrimitive.content)
            .isEqualTo(DaemonProtocol.PROTOCOL_VERSION.toString())
    }
}
