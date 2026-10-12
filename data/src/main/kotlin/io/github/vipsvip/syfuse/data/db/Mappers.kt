package io.github.vipsvip.syfuse.data.db

import io.github.vipsvip.syfuse.domain.model.DnsConfig
import io.github.vipsvip.syfuse.domain.model.InstalledKind
import io.github.vipsvip.syfuse.domain.model.InstalledManifest
import io.github.vipsvip.syfuse.domain.model.NotifyAction
import io.github.vipsvip.syfuse.domain.model.NotifyRule
import io.github.vipsvip.syfuse.domain.model.PluginOptions
import io.github.vipsvip.syfuse.domain.model.Profile
import io.github.vipsvip.syfuse.domain.model.ProfileType
import io.github.vipsvip.syfuse.domain.model.ProxyGroup
import io.github.vipsvip.syfuse.domain.model.ProxyGroupType
import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import io.github.vipsvip.syfuse.domain.model.RewriteAction
import io.github.vipsvip.syfuse.domain.model.RewriteEntry
import io.github.vipsvip.syfuse.domain.model.RoutingMode
import io.github.vipsvip.syfuse.domain.model.RuleAction
import io.github.vipsvip.syfuse.domain.model.RuleEntry
import io.github.vipsvip.syfuse.domain.model.RuleKind
import io.github.vipsvip.syfuse.domain.model.RuleSet
import io.github.vipsvip.syfuse.domain.model.RuleSetFormat
import io.github.vipsvip.syfuse.domain.model.ScriptEntry
import io.github.vipsvip.syfuse.domain.model.ScriptHook
import io.github.vipsvip.syfuse.domain.model.SplashAction
import io.github.vipsvip.syfuse.domain.model.SplashMatchType
import io.github.vipsvip.syfuse.domain.model.SplashSkipRule
import io.github.vipsvip.syfuse.domain.model.SubscriptionInfo
import io.github.vipsvip.syfuse.domain.model.TlsOptions
import io.github.vipsvip.syfuse.domain.model.TrafficStat
import io.github.vipsvip.syfuse.domain.model.TransportOptions
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/**
 * 实体 ↔ 领域模型映射。
 *
 * 所有枚举以**名称字符串**落库（而不是序号）：序号会随枚举重排而静默错位，
 * 名称则会在枚举改名时立刻抛出，属于"失败得响亮"的设计。
 */
internal object DbJson {
    val json: Json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
    }
}

internal inline fun <reified T : Enum<T>> String?.enumOr(fallback: T): T =
    this?.let { runCatching { enumValueOf<T>(it) }.getOrNull() } ?: fallback

// ------------------------------------------------------------------ Profile

fun ProfileEntity.toDomain(): Profile = Profile(
    id = id,
    name = name,
    type = type.enumOr(ProfileType.LOCAL),
    sourceUrl = sourceUrl,
    updateIntervalHours = updateIntervalHours,
    subscriptionInfo = subscriptionJson?.let { DbJson.json.decodeFromString<SubscriptionInfo>(it) },
    rawConfig = rawConfig,
    routingMode = routingMode.enumOr(RoutingMode.RULE),
    ruleSetIds = DbJson.json.decodeFromString(ruleSetIdsJson),
    dnsConfig = DbJson.json.decodeFromString(dnsJson),
    scriptIds = DbJson.json.decodeFromString(scriptIdsJson),
    createdAt = createdAt,
    updatedAt = updatedAt,
)

fun Profile.toEntity(): ProfileEntity = ProfileEntity(
    id = id,
    name = name,
    type = type.name,
    sourceUrl = sourceUrl,
    updateIntervalHours = updateIntervalHours,
    routingMode = routingMode.name,
    rawConfig = rawConfig,
    subscriptionJson = subscriptionInfo?.let { DbJson.json.encodeToString(SubscriptionInfo.serializer(), it) },
    dnsJson = DbJson.json.encodeToString(DnsConfig.serializer(), dnsConfig),
    ruleSetIdsJson = DbJson.json.encodeToString(ruleSetIds),
    scriptIdsJson = DbJson.json.encodeToString(scriptIds),
    createdAt = createdAt,
    updatedAt = updatedAt,
)

// --------------------------------------------------------------- ProxyNode

fun ProxyNodeEntity.toDomain(): ProxyNode = ProxyNode(
    id = id,
    name = name,
    protocol = protocol.enumOr(ProxyProtocol.SOCKS5),
    server = server,
    port = port,
    credentialRef = credentialRef,
    tls = tlsJson?.let { DbJson.json.decodeFromString<TlsOptions>(it) },
    transport = transportJson?.let { DbJson.json.decodeFromString<TransportOptions>(it) },
    plugin = pluginJson?.let { DbJson.json.decodeFromString<PluginOptions>(it) },
    tags = DbJson.json.decodeFromString(tagsJson),
    latencyMs = latencyMs,
    favorite = favorite,
    sortIndex = sortIndex,
    rawJson = rawJson,
)

fun ProxyNode.toEntity(): ProxyNodeEntity = ProxyNodeEntity(
    id = id,
    name = name,
    protocol = protocol.name,
    server = server,
    port = port,
    credentialRef = credentialRef,
    tlsJson = tls?.let { DbJson.json.encodeToString(TlsOptions.serializer(), it) },
    transportJson = transport?.let { DbJson.json.encodeToString(TransportOptions.serializer(), it) },
    pluginJson = plugin?.let { DbJson.json.encodeToString(PluginOptions.serializer(), it) },
    tagsJson = DbJson.json.encodeToString(tags),
    latencyMs = latencyMs,
    favorite = favorite,
    sortIndex = sortIndex,
    rawJson = rawJson,
)

// -------------------------------------------------------------- ProxyGroup

fun ProxyGroupEntity.toDomain(): ProxyGroup = ProxyGroup(
    id = id,
    name = name,
    type = type.enumOr(ProxyGroupType.SELECT),
    members = DbJson.json.decodeFromString(membersJson),
    url = url,
    testIntervalSeconds = testIntervalSeconds,
    toleranceMs = toleranceMs,
    timeoutMs = timeoutMs,
    dialerProxy = dialerProxy,
    nested = nested,
)

fun ProxyGroup.toEntity(): ProxyGroupEntity = ProxyGroupEntity(
    id = id,
    name = name,
    type = type.name,
    membersJson = DbJson.json.encodeToString(members),
    url = url,
    testIntervalSeconds = testIntervalSeconds,
    toleranceMs = toleranceMs,
    timeoutMs = timeoutMs,
    dialerProxy = dialerProxy,
    nested = nested,
)

// ----------------------------------------------------------------- RuleSet

fun RuleSetEntity.toDomain(): RuleSet = RuleSet(
    id = id,
    name = name,
    sourceUrl = sourceUrl,
    localPath = localPath,
    format = format.enumOr(RuleSetFormat.DOMAIN),
    updateIntervalHours = updateIntervalHours,
    ruleCount = ruleCount,
    lastUpdated = lastUpdated,
    sha256 = sha256,
    enabled = enabled,
)

fun RuleSet.toEntity(): RuleSetEntity = RuleSetEntity(
    id = id,
    name = name,
    sourceUrl = sourceUrl,
    localPath = localPath,
    format = format.name,
    updateIntervalHours = updateIntervalHours,
    ruleCount = ruleCount,
    lastUpdated = lastUpdated,
    sha256 = sha256,
    enabled = enabled,
)

// --------------------------------------------------------------- RuleEntry

fun RuleEntryEntity.toDomain(): RuleEntry = RuleEntry(
    id = id,
    sourceId = sourceId,
    rawText = rawText,
    kind = kind.enumOr(RuleKind.FILTER),
    action = action.enumOr(RuleAction.BLOCK),
    lineNo = lineNo,
    hitCount = hitCount,
    order = order,
    modifierText = modifierText,
)

fun RuleEntry.toEntity(): RuleEntryEntity = RuleEntryEntity(
    id = id,
    sourceId = sourceId,
    rawText = rawText,
    kind = kind.name,
    action = action.name,
    lineNo = lineNo,
    hitCount = hitCount,
    order = order,
    modifierText = modifierText,
)

// ------------------------------------------------------------------ Script

fun ScriptEntity.toDomain(): ScriptEntry = ScriptEntry(
    id = id,
    name = name,
    content = content,
    hooks = DbJson.json.decodeFromString<List<ScriptHook>>(hooksJson),
    argument = argument,
    enabled = enabled,
    order = order,
    lastRunStatus = lastRunStatus,
    lastRunAt = lastRunAt,
)

fun ScriptEntry.toEntity(): ScriptEntity = ScriptEntity(
    id = id,
    name = name,
    content = content,
    hooksJson = DbJson.json.encodeToString(hooks),
    argument = argument,
    enabled = enabled,
    order = order,
    lastRunStatus = lastRunStatus,
    lastRunAt = lastRunAt,
)

// ----------------------------------------------------------------- Rewrite

fun RewriteEntity.toDomain(): RewriteEntry = RewriteEntry(
    id = id,
    match = match,
    actionType = actionType.enumOr(RewriteAction.REJECT),
    actionValue = actionValue,
    enabled = enabled,
)

fun RewriteEntry.toEntity(): RewriteEntity = RewriteEntity(
    id = id,
    match = match,
    actionType = actionType.name,
    actionValue = actionValue,
    enabled = enabled,
)

// -------------------------------------------------------------- SplashSkip

fun SplashRuleEntity.toDomain(): SplashSkipRule = SplashSkipRule(
    id = id,
    packageName = packageName,
    matchType = matchType.enumOr(SplashMatchType.ACTIVITY_NAME),
    matchValue = matchValue,
    action = action.enumOr(SplashAction.CLICK_SKIP),
    delayMs = delayMs,
    cooldownMs = cooldownMs,
    enabled = enabled,
    hitCount = hitCount,
)

fun SplashSkipRule.toEntity(): SplashRuleEntity = SplashRuleEntity(
    id = id,
    packageName = packageName,
    matchType = matchType.name,
    matchValue = matchValue,
    action = action.name,
    delayMs = delayMs,
    cooldownMs = cooldownMs,
    enabled = enabled,
    hitCount = hitCount,
)

// ------------------------------------------------------------- NotifyRule

fun NotifyRuleEntity.toDomain(): NotifyRule = NotifyRule(
    id = id,
    packageName = packageName,
    keyword = keyword,
    regex = regex,
    action = action.enumOr(NotifyAction.FOLD),
    enabled = enabled,
)

fun NotifyRule.toEntity(): NotifyRuleEntity = NotifyRuleEntity(
    id = id,
    packageName = packageName,
    keyword = keyword,
    regex = regex,
    action = action.name,
    enabled = enabled,
)

// ------------------------------------------------------------ TrafficStat

fun TrafficStatEntity.toDomain(): TrafficStat = TrafficStat(
    ts = ts,
    appUid = appUid,
    packageName = packageName,
    host = host,
    ruleId = ruleId,
    chain = chain,
    upBytes = upBytes,
    downBytes = downBytes,
    durationMs = durationMs,
)

fun TrafficStat.toEntity(): TrafficStatEntity = TrafficStatEntity(
    ts = ts,
    appUid = appUid,
    packageName = packageName,
    host = host,
    ruleId = ruleId,
    chain = chain,
    upBytes = upBytes,
    downBytes = downBytes,
    durationMs = durationMs,
)

// ------------------------------------------------------ InstalledManifest

fun InstalledManifestEntity.toDomain(): InstalledManifest = InstalledManifest(
    entryId = entryId,
    kind = kind.enumOr(InstalledKind.MOUNT),
    target = target,
    source = source,
    restorable = restorable,
    createdAt = createdAt,
)

fun InstalledManifest.toEntity(): InstalledManifestEntity = InstalledManifestEntity(
    entryId = entryId,
    kind = kind.name,
    target = target,
    source = source,
    restorable = restorable,
    createdAt = createdAt,
)
