package io.github.vipsvip.syfuse.domain.config

import io.github.vipsvip.syfuse.core.common.ErrorCode
import io.github.vipsvip.syfuse.core.common.SyfuseError
import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.domain.model.ConfigBundle
import io.github.vipsvip.syfuse.domain.model.SCHEMA_VERSION
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put

/**
 * 配置包的编解码。
 *
 * 第 10 部分要求三条兼容性规则，这里是它们的实现位置：
 *  1. **版本字段**：解码时读 `schemaVersion`，高于本版本直接拒绝而不是猜测字段含义；
 *  2. **旧版本自动迁移**：低版本走 [migrate]，当前只需补齐缺失字段；
 *  3. **未知字段保留不丢弃**：本版本不认识的顶层键被收进 [ConfigBundle.rawExtras]，
 *     导出时原样写回。这样"新版本导出的配置在旧版本里过一遍"不会丢数据。
 */
class ConfigBundleCodec(
    private val json: Json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
        prettyPrint = true
    },
) {

    private val knownTopLevelKeys = setOf(
        "schemaVersion",
        "profile",
        "nodes",
        "groups",
        "ruleSets",
        "rules",
        "scripts",
        "rewrites",
        "filters",
        "rawExtras",
    )

    fun encode(bundle: ConfigBundle): String {
        val base = json.encodeToJsonElement(ConfigBundle.serializer(), bundle.copy(rawExtras = emptyMap()))
            .jsonObject
        // 把此前保留的未知字段写回，且不覆盖已知键——已知键以当前 schema 为准
        val merged = buildJsonObject {
            base.forEach { (key, value) -> put(key, value) }
            bundle.rawExtras.forEach { (key, value) ->
                if (key !in knownTopLevelKeys) {
                    runCatching { put(key, json.parseToJsonElement(value)) }
                }
            }
        }
        return json.encodeToString(JsonObject.serializer(), merged)
    }

    fun decode(text: String): SyfuseResult<ConfigBundle> {
        if (text.isBlank()) {
            return fail(ErrorCode.CONFIG_INVALID, "配置内容为空", "配置内容为空，无法导入", "请确认文件未被截断")
        }

        val root = runCatching { json.parseToJsonElement(text) }
            .getOrElse { t ->
                return fail(
                    ErrorCode.CONFIG_INVALID,
                    "JSON 解析失败：${t.message}",
                    "配置文件不是合法的 JSON",
                    "请用文本编辑器检查括号与逗号是否配对",
                )
            }

        val obj = root as? JsonObject
            ?: return fail(ErrorCode.CONFIG_INVALID, "顶层不是 JSON 对象", "配置文件结构不正确")

        val version = obj["schemaVersion"]?.jsonPrimitive?.intOrNull ?: 1
        if (version > SCHEMA_VERSION) {
            return fail(
                ErrorCode.CONFIG_VERSION_UNSUPPORTED,
                "配置版本 $version 高于本应用支持的 $SCHEMA_VERSION",
                "这份配置来自更新版本的 Syfuse，当前版本无法安全读取",
                "请升级应用后再导入；直接导入可能丢失设置",
            )
        }

        val decoded = runCatching { json.decodeFromJsonElement(ConfigBundle.serializer(), obj) }
            .getOrElse { t ->
                return fail(
                    ErrorCode.CONFIG_INVALID,
                    "字段解析失败：${t.message}",
                    "配置字段与当前 schema 不匹配",
                    "请检查 profile / nodes 等字段的类型",
                )
            }

        // 未知顶层字段保留原文
        val extras = obj.filterKeys { it !in knownTopLevelKeys }
            .mapValues { (_, value) -> value.toString() }

        return SyfuseResult.Success(
            migrate(decoded.copy(rawExtras = extras), fromVersion = version),
        )
    }

    /**
     * 版本迁移。当前 schema 只有 v1，因此迁移动作是"补齐缺失字段 + 记录来源版本"。
     * 将来新增 v2 时在这里按 fromVersion 逐级升级，而不是在 decode 里散落 if。
     */
    private fun migrate(bundle: ConfigBundle, fromVersion: Int): ConfigBundle =
        bundle.copy(schemaVersion = SCHEMA_VERSION)

    private fun fail(
        code: ErrorCode,
        technical: String,
        userMessage: String,
        hint: String? = null,
    ): SyfuseResult.Failure = SyfuseResult.Failure(
        SyfuseError(code = code, technical = technical, userMessage = userMessage, hint = hint),
    )
}
