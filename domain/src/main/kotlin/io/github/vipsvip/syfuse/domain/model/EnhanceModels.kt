package io.github.vipsvip.syfuse.domain.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** 脚本（5.6 / 8.8）。 */
@Serializable
data class ScriptEntry(
    val id: String,
    val name: String,
    val content: String,
    val hooks: List<ScriptHook> = emptyList(),
    val argument: String? = null,
    val enabled: Boolean = true,
    val order: Int = 0,
    val lastRunStatus: String? = null,
    val lastRunAt: Long = 0L,
)

/** 脚本 Hook 点，与 5.6 一致。 */
@Serializable
enum class ScriptHook {
    @SerialName("http-request") HTTP_REQUEST,
    @SerialName("http-response") HTTP_RESPONSE,
    @SerialName("rule") RULE,
    @SerialName("dns") DNS,
    @SerialName("cron") CRON,
}

/** URL 重写（5.6）。 */
@Serializable
data class RewriteEntry(
    val id: String,
    val match: String,
    val actionType: RewriteAction,
    val actionValue: String? = null,
    val enabled: Boolean = true,
)

/** 重写动作，与 5.6 列出的动作集一致。 */
@Serializable
enum class RewriteAction {
    @SerialName("reject") REJECT,
    @SerialName("reject-200") REJECT_200,
    @SerialName("reject-dict") REJECT_DICT,
    @SerialName("reject-array") REJECT_ARRAY,
    @SerialName("302") REDIRECT_302,
    @SerialName("307") REDIRECT_307,
    @SerialName("request-header") REQUEST_HEADER,
    @SerialName("response-header") RESPONSE_HEADER,
    @SerialName("request-body") REQUEST_BODY,
    @SerialName("response-body") RESPONSE_BODY,
}

/** MITM 配置（5.6）。 */
@Serializable
data class MitmConfig(
    val enabled: Boolean = false,
    /** 解密域名白名单，支持通配符。 */
    val hostnames: List<String> = emptyList(),
    val caFingerprint: String? = null,
    val caInstalled: Boolean = false,
    val httpsPort: Int = 8443,
    val httpPort: Int = 8080,
    /** 证书固定策略：passthrough / verify / ignore。 */
    val pinningPolicy: PinningPolicy = PinningPolicy.PASSTHROUGH,
    /** 按应用分流：留空表示全局（排除 root 自身）。 */
    val apps: List<String> = emptyList(),
)

@Serializable
enum class PinningPolicy {
    @SerialName("passthrough") PASSTHROUGH,
    @SerialName("verify") VERIFY,
    @SerialName("ignore") IGNORE,
}

/** 过滤配置（6.1，差异增强层）。 */
@Serializable
data class FilterConfig(
    val id: String,
    val name: String,
    val sources: List<FilterSource> = emptyList(),
    val enabled: Boolean = true,
    val updateIntervalHours: Int = 0,
)

@Serializable
data class FilterSource(
    val url: String? = null,
    val localPath: String? = null,
    val format: RuleSetFormat = RuleSetFormat.CLASSICAL,
    val sha256: String? = null,
)

/** 开屏跳过规则（6.2）。 */
@Serializable
data class SplashSkipRule(
    val id: String,
    val packageName: String,
    val matchType: SplashMatchType = SplashMatchType.ACTIVITY_NAME,
    val matchValue: String,
    val action: SplashAction = SplashAction.CLICK_SKIP,
    val delayMs: Int = 800,
    val cooldownMs: Int = 3000,
    val enabled: Boolean = false,
    val hitCount: Long = 0L,
)

@Serializable
enum class SplashMatchType {
    @SerialName("activity-name") ACTIVITY_NAME,
    @SerialName("sdk-call") SDK_CALL,
    @SerialName("view-id") VIEW_ID,
}

@Serializable
enum class SplashAction {
    @SerialName("click-skip") CLICK_SKIP,
    @SerialName("finish-activity") FINISH_ACTIVITY,
    @SerialName("back-key") BACK_KEY,
}

/** 通知净化规则（6.3）。 */
@Serializable
data class NotifyRule(
    val id: String,
    val packageName: String? = null,
    val keyword: String? = null,
    val regex: String? = null,
    val action: NotifyAction = NotifyAction.FOLD,
    val enabled: Boolean = false,
)

@Serializable
enum class NotifyAction {
    @SerialName("fold") FOLD,
    @SerialName("block") BLOCK,
}
