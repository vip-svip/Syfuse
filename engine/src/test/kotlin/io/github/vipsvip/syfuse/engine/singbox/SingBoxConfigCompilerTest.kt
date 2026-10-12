package io.github.vipsvip.syfuse.engine.singbox

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.core.result.SyfuseResult
import io.github.vipsvip.syfuse.domain.engine.DnsSpec
import io.github.vipsvip.syfuse.domain.engine.EngineRuntimeConfig
import io.github.vipsvip.syfuse.domain.engine.GroupSpec
import io.github.vipsvip.syfuse.domain.engine.InboundSpec
import io.github.vipsvip.syfuse.domain.engine.LogLevel
import io.github.vipsvip.syfuse.domain.engine.OutboundSpec
import io.github.vipsvip.syfuse.domain.engine.RouteRuleSpec
import io.github.vipsvip.syfuse.domain.engine.RunMode
import io.github.vipsvip.syfuse.domain.model.RoutingMode
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Test

/**
 * [SingBoxConfigCompiler] 单测。
 *
 * 重点覆盖三类失败路径，因为它们是最容易静默出错的：空入站、tag 重复、
 * 以及必须存在的 direct 兜底出站（缺了它流量会被丢弃而非直连）。
 */
class SingBoxConfigCompilerTest {

    private val compiler = SingBoxConfigCompiler()
    private val json = Json { ignoreUnknownKeys = true }

    private fun baseConfig(): EngineRuntimeConfig = EngineRuntimeConfig(
        mode = RunMode.VPN,
        logLevel = LogLevel.WARN,
        outbounds = listOf(
            OutboundSpec(
                tag = "node-1",
                type = "vless",
                server = "example.com",
                port = 443,
                raw = mapOf("uuid" to "00000000-0000-0000-0000-000000000000"),
            ),
        ),
        groups = listOf(
            GroupSpec(tag = "proxy", type = "selector", members = listOf("node-1")),
        ),
        rules = listOf(
            RouteRuleSpec(kind = "domain_suffix", payload = "cn", action = "direct"),
        ),
        dns = DnsSpec(servers = listOf("1.1.1.1"), mode = "prefer_ipv4"),
        inbounds = listOf(
            InboundSpec(type = "tun", listen = "0.0.0.0", port = 0),
        ),
    )

    @Test
    fun `编译成功时产出合法 JSON 且含 direct 兜底出站`() {
        val result = compiler.compile(baseConfig())
        assertThat(result).isInstanceOf(SyfuseResult.Success::class.java)

        val text = (result as SyfuseResult.Success).value
        val root = json.parseToJsonElement(text) as JsonObject

        val outboundTags = root["outbounds"]!!.jsonArray
            .map { it.jsonObjectTag() }
        // direct 必须存在，否则未命中规则的流量无处可去
        assertThat(outboundTags).contains("direct")
        assertThat(outboundTags).contains("node-1")
        assertThat(outboundTags).contains("proxy")
    }

    @Test
    fun `入站为空时返回 Validation 失败而非产出静默不可用的配置`() {
        val config = baseConfig().copy(inbounds = emptyList())
        val result = compiler.compile(config)

        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        assertThat(error).isInstanceOf(io.github.vipsvip.syfuse.core.result.SyfuseError.Validation::class.java)
        assertThat(error.message).contains("入站")
    }

    @Test
    fun `出站 tag 重复时返回 Validation 失败并指出重复的 tag`() {
        val config = baseConfig().copy(
            outbounds = listOf(
                OutboundSpec("dup", "vless", "a.com", 443, emptyMap()),
                OutboundSpec("dup", "trojan", "b.com", 443, emptyMap()),
            ),
        )
        val result = compiler.compile(config)

        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        assertThat(error.message).contains("dup")
    }

    @Test
    fun `节点与策略组的原始字段被透传不被抽象层削掉`() {
        val config = baseConfig().copy(
            outbounds = listOf(
                OutboundSpec(
                    tag = "node-x",
                    type = "hysteria2",
                    server = "h.com",
                    port = 8443,
                    raw = mapOf("password" to "secret", "obfs" to "salamander"),
                ),
            ),
        )
        val result = compiler.compile(config) as SyfuseResult.Success
        val root = json.parseToJsonElement(result.value) as JsonObject

        val nodeX = root["outbounds"]!!.jsonArray
            .first { it.jsonObjectTag() == "node-x" }
        // 这条断言是「覆盖 getOrNull 之外协议细节」的保障
        assertThat(nodeX.toString()).contains("salamander")
        assertThat(nodeX.toString()).contains("secret")
    }

    @Test
    fun `route final 兜底指向 direct`() {
        val result = compiler.compile(baseConfig()) as SyfuseResult.Success
        val root = json.parseToJsonElement(result.value) as JsonObject
        val route = root["route"] as JsonObject
        assertThat(route["final"]!!.jsonPrimitive.content).isEqualTo("direct")
    }

    /** 取 JsonObject 的 tag 字段，避免测试里到处写类型转换。 */
    private fun kotlinx.serialization.json.JsonElement.jsonObjectTag(): String =
        (this as JsonObject)["tag"]!!.jsonPrimitive.content

    /** 该测试用到的枚举需保持稳定，否则配置编译契约被破坏。 */
    @Test
    fun `路由模式命名保持与 Shadowrocket 概念同名`() {
        assertThat(RoutingMode.RULE.displayName).isEqualTo("配置")
        assertThat(RoutingMode.GLOBAL.displayName).isEqualTo("代理")
        assertThat(RoutingMode.DIRECT.displayName).isEqualTo("直连")
    }
}
