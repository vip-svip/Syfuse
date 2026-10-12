package io.github.vipsvip.syfuse.engine.singbox

import io.github.vipsvip.syfuse.core.result.SyfuseError
import io.github.vipsvip.syfuse.core.result.SyfuseResult
import io.github.vipsvip.syfuse.domain.engine.EngineConfigCompiler
import io.github.vipsvip.syfuse.domain.engine.EngineKind
import io.github.vipsvip.syfuse.domain.engine.EngineRuntimeConfig
import io.github.vipsvip.syfuse.domain.engine.RunMode
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * sing-box 配置编译器：把内核无关的 [EngineRuntimeConfig] 编译为 sing-box JSON。
 *
 * ## 设计取舍
 *
 * 用 kotlinx.serialization 的 JSON DSL 手工构造，而非定义一整套 @Serializable 数据类。
 * 理由：sing-box 的配置 schema 演进频繁，且存在大量「未知字段透传」需求
 * （提示词 10 明确要求未知字段保留不丢弃）。手工构造更容易做到「原始字段原样带过」，
 * 且编译失败时能精确指出是哪个字段（返回带字段名的 Validation 错误）。
 *
 * ## 为什么这个类在 M1 是「真实现」而非桩
 *
 * 配置编译是纯函数逻辑，不依赖内核二进制，因此可以在 M1 完整实现并单测。
 * 这样 M2 接入内核时，只需验证「编译产物能被内核接受」，而不必同时调试编译逻辑。
 */
class SingBoxConfigCompiler : EngineConfigCompiler {

    override val kind: EngineKind = EngineKind.SING_BOX

    private val json = Json {
        prettyPrint = true
        // 不忽略未知键，保证 rawExtras 透传时字段完整
        ignoreUnknownKeys = false
    }

    override fun compile(config: EngineRuntimeConfig): SyfuseResult<String> {
        // 前置校验：入站为空会导致内核启动后无任何监听，属静默失败，必须提前拦下
        if (config.inbounds.isEmpty()) {
            return SyfuseResult.Failure(
                SyfuseError.Validation("inbounds", "入站列表为空，内核启动后将无可用监听端口"),
            )
        }

        // tag 唯一性校验：重复 tag 会让规则与策略组指向歧义，且内核报错信息难以定位
        val duplicateTag = findDuplicateTag(config)
        if (duplicateTag != null) {
            return SyfuseResult.Failure(
                SyfuseError.Validation("outbounds", "出站 tag 重复：$duplicateTag"),
            )
        }

        return try {
            val root = buildJsonObject {
                put("log", buildJsonObject {
                    put("level", config.logLevel.name.lowercase())
                    put("timestamp", true)
                })

                put("dns", buildDns(config))
                put("inbounds", buildInbounds(config))
                put("outbounds", buildOutbounds(config))
                put("route", buildRoute(config))

                // 用户自定义的额外顶层配置最后写入，允许其覆盖上述默认值。
                // 这是「抽象层不削能力」的兜底：内核新特性无需改本代码即可用。
                config.rawExtras.forEach { (key, rawJsonText) ->
                    runCatching { json.parseToJsonElement(rawJsonText) }
                        .onSuccess { put(key, it) }
                }
            }
            SyfuseResult.Success(json.encodeToString(JsonObject.serializer(), root))
        } catch (e: Exception) {
            // 编译期异常属编程错误或极端的输入组合，仍如实报告而非吞掉
            SyfuseResult.Failure(SyfuseError.Parse("syfuse-config", "配置编译失败：${e.message}"))
        }
    }

    private fun findDuplicateTag(config: EngineRuntimeConfig): String? {
        val tags = config.outbounds.map { it.tag }
        return tags.groupingBy { it }.eachCount().entries.firstOrNull { it.value > 1 }?.key
    }

    private fun buildDns(config: EngineRuntimeConfig): JsonObject = buildJsonObject {
        put("servers", buildJsonArray {
            config.dns.servers.forEach { server ->
                add(buildJsonObject { put("address", server) })
            }
        })
        config.dns.fakeIpPool?.let { pool ->
            // fake-ip 是 DNS 模式而非独立服务器，这里用 sing-box 的 inbound 语法表达
            put("fakeip", buildJsonObject {
                put("enabled", true)
                put("inet4_range", pool)
            })
        }
        if (config.dns.hosts.isNotEmpty()) {
            put("rules", buildJsonArray {
                config.dns.hosts.forEach { (domain, address) ->
                    add(buildJsonObject {
                        put("domain", domain)
                        put("action", "route")
                        put("server", address)
                    })
                }
            })
        }
        put("strategy", config.dns.mode)
        put("independent_cache", true)
    }

    private fun buildInbounds(config: EngineRuntimeConfig): JsonArray = buildJsonArray {
        config.inbounds.forEach { inbound ->
            add(buildJsonObject {
                put("type", inbound.type)
                put("tag", "in-${inbound.type}-${inbound.port}")
                put("listen", inbound.listen)
                put("listen_port", inbound.port)

                // TUN 模式专属字段：VPN 与 root 两种模式在此分叉
                if (inbound.type == "tun") {
                    put("stack", inbound.options["stack"] ?: "mixed")
                    put("auto_route", inbound.options["auto_route"]?.toBoolean() ?: true)
                    put("strict_route", inbound.options["strict_route"]?.toBoolean() ?: false)
                    inbound.options["mtu"]?.toIntOrNull()?.let { put("mtu", it) }
                    put("address", buildJsonArray {
                        add(kotlinx.serialization.json.JsonPrimitive("172.19.0.1/30"))
                        add(kotlinx.serialization.json.JsonPrimitive("fdfe:dcba:9876::1/126"))
                    })
                }
                inbound.options.forEach { (k, v) ->
                    if (k !in TUN_RESERVED_OPTIONS) put(k, v)
                }
            })
        }
    }

    private fun buildOutbounds(config: EngineRuntimeConfig): JsonArray = buildJsonArray {
        // direct 出站必须存在：所有「直连」规则与 fallback 都由它兜底
        add(buildJsonObject {
            put("type", "direct")
            put("tag", DIRECT_TAG)
        })

        config.outbounds.forEach { outbound ->
            add(buildJsonObject {
                put("type", outbound.type)
                put("tag", outbound.tag)
                put("server", outbound.server)
                put("server_port", outbound.port)
                outbound.raw.forEach { (k, v) -> put(k, v) }
            })
        }

        config.groups.forEach { group ->
            add(buildJsonObject {
                put("type", group.type)
                put("tag", group.tag)
                put("outbounds", buildJsonArray {
                    group.members.forEach { add(kotlinx.serialization.json.JsonPrimitive(it)) }
                })
                group.url?.let { put("url", it) }
                group.intervalSeconds?.let { put("interval", "${it}s") }
                group.toleranceMillis?.let { put("tolerance", it) }
            })
        }
    }

    private fun buildRoute(config: EngineRuntimeConfig): JsonObject = buildJsonObject {
        put("rules", buildJsonArray {
            config.rules.forEach { rule ->
                add(buildJsonObject {
                    put(rule.kind, rule.payload)
                    put("outbound", rule.action)
                    rule.extras.forEach { (k, v) -> put(k, v) }
                })
            }
        })
        // 规则全部未命中时的兜底，避免无出站导致流量被丢弃
        put("final", DIRECT_TAG)
        put("auto_detect_interface", true)
    }

    private companion object {
        const val DIRECT_TAG = "direct"

        /** TUN 入站已在上面显式处理的字段，避免重复写入。 */
        val TUN_RESERVED_OPTIONS = setOf("stack", "auto_route", "strict_route", "mtu")
    }
}
