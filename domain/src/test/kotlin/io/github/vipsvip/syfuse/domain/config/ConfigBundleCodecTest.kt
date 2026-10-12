package io.github.vipsvip.syfuse.domain.config

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.domain.model.ConfigBundle
import io.github.vipsvip.syfuse.domain.model.Profile
import io.github.vipsvip.syfuse.domain.model.ProfileType
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import io.github.vipsvip.syfuse.domain.model.RoutingMode
import org.junit.jupiter.api.Test

/**
 * 配置包 round-trip 测试。
 * 第 10 部分的硬要求是"导入导出字段级等值"，这里把它变成可执行断言。
 */
class ConfigBundleCodecTest {

    private val codec = ConfigBundleCodec()

    private fun sampleBundle(): ConfigBundle = ConfigBundle(
        profile = Profile(
            id = "p1",
            name = "主配置",
            type = ProfileType.REMOTE,
            sourceUrl = "https://example.invalid/sub",
            updateIntervalHours = 24,
            routingMode = RoutingMode.RULE,
            dnsConfig = io.github.vipsvip.syfuse.domain.model.DnsConfig(cacheSize = 8192),
        ),
        nodes = listOf(
            ProxyNode(
                id = "n1",
                name = "香港 01",
                protocol = ProxyProtocol.VLESS,
                server = "hk1.example.invalid",
                port = 443,
                credentialRef = "vault:node-n1",
                tags = listOf("hk", "premium"),
                sortIndex = 3,
            ),
            ProxyNode(
                id = "n2",
                name = "日本 02",
                protocol = ProxyProtocol.HYSTERIA2,
                server = "jp2.example.invalid",
                port = 8443,
                latencyMs = 88,
                favorite = true,
            ),
        ),
    )

    @Test
    fun `编码后解码得到字段级等值`() {
        val original = sampleBundle()
        val text = codec.encode(original)

        val decoded = codec.decode(text)
        assertThat(decoded).isInstanceOf(SyfuseResult.Success::class.java)
        val restored = (decoded as SyfuseResult.Success).value

        assertThat(restored.profile).isEqualTo(original.profile)
        assertThat(restored.nodes).isEqualTo(original.nodes)
        assertThat(restored.schemaVersion).isEqualTo(original.schemaVersion)
    }

    @Test
    fun `未知顶层字段在 round-trip 后被保留`() {
        val withUnknown = """
            {
              "schemaVersion": 1,
              "nodes": [],
              "futureField": {"nested": [1, 2, 3]},
              "anotherUnknown": "keep-me"
            }
        """.trimIndent()

        val decoded = codec.decode(withUnknown)
        val bundle = (decoded as SyfuseResult.Success).value
        assertThat(bundle.rawExtras.keys).containsExactly("futureField", "anotherUnknown")

        val reencoded = codec.encode(bundle)
        val redecoded = (codec.decode(reencoded) as SyfuseResult.Success).value
        assertThat(redecoded.rawExtras.keys).containsExactly("futureField", "anotherUnknown")
        assertThat(reencoded).contains("keep-me")
    }

    @Test
    fun `高于当前 schema 版本被拒绝而不是猜测`() {
        val future = """{"schemaVersion": 99, "nodes": []}"""
        val result = codec.decode(future)

        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        assertThat(error.code).isEqualTo(ErrorCode.CONFIG_VERSION_UNSUPPORTED)
        assertThat(error.hint).isNotNull()
    }

    @Test
    fun `非法 JSON 给出可读错误而不是崩溃`() {
        val result = codec.decode("{ not json ")
        assertThat(result).isInstanceOf(SyfuseResult.Failure::class.java)
        val error = (result as SyfuseResult.Failure).error
        assertThat(error.code).isEqualTo(ErrorCode.CONFIG_INVALID)
        assertThat(error.userMessage).isNotEmpty()
    }

    @Test
    fun `空内容被拒绝`() {
        assertThat(codec.decode("   ")).isInstanceOf(SyfuseResult.Failure::class.java)
    }

    @Test
    fun `缺少版本字段时按 v1 处理`() {
        val noVersion = """{"nodes": []}"""
        val result = codec.decode(noVersion)
        assertThat(result).isInstanceOf(SyfuseResult.Success::class.java)
        assertThat((result as SyfuseResult.Success).value.schemaVersion).isEqualTo(1)
    }
}
