package io.github.vipsvip.syfuse.data.db

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/**
 * Room 实体。
 *
 * 设计取舍：嵌套结构（TLS / 传输 / DNS / 订阅信息）统一以 JSON 字符串列存储，
 * 而不是拆成多张关联表。理由：
 *  ① 这些结构随内核演进而变，JSON 列使 schema 变更不需要迁移脚本；
 *  ② 它们的查询需求是"整体读写"而非"按字段筛选"；
 *  ③ 与导入导出的 round-trip 保持同一份序列化逻辑，减少两处不一致的风险。
 * 需要按字段查询的部分（配置、节点、规则）才拆成独立列并建索引。
 */

@Entity(tableName = "profiles")
data class ProfileEntity(
    @PrimaryKey val id: String,
    val name: String,
    val type: String,
    val sourceUrl: String?,
    val updateIntervalHours: Int,
    val routingMode: String,
    val rawConfig: String,
    val subscriptionJson: String?,
    val dnsJson: String,
    val ruleSetIdsJson: String,
    val scriptIdsJson: String,
    val createdAt: Long,
    val updatedAt: Long,
)

@Entity(
    tableName = "proxy_nodes",
    indices = [Index("server"), Index("protocol"), Index("sortIndex")],
)
data class ProxyNodeEntity(
    @PrimaryKey val id: String,
    val name: String,
    val protocol: String,
    val server: String,
    val port: Int,
    /** 指向凭据保险箱的引用，**不是**明文凭据（R5）。 */
    val credentialRef: String?,
    val tlsJson: String?,
    val transportJson: String?,
    val pluginJson: String?,
    val tagsJson: String,
    val latencyMs: Int?,
    val favorite: Boolean,
    val sortIndex: Int,
    val rawJson: String,
)

@Entity(tableName = "proxy_groups")
data class ProxyGroupEntity(
    @PrimaryKey val id: String,
    val name: String,
    val type: String,
    val membersJson: String,
    val url: String?,
    val testIntervalSeconds: Int?,
    val toleranceMs: Int?,
    val timeoutMs: Int?,
    val dialerProxy: String?,
    val nested: Boolean,
)

@Entity(tableName = "rule_sets")
data class RuleSetEntity(
    @PrimaryKey val id: String,
    val name: String,
    val sourceUrl: String?,
    val localPath: String?,
    val format: String,
    val updateIntervalHours: Int,
    val ruleCount: Int,
    val lastUpdated: Long,
    val sha256: String?,
    val enabled: Boolean,
)

@Entity(
    tableName = "rule_entries",
    indices = [Index("sourceId")],
)
data class RuleEntryEntity(
    @PrimaryKey val id: String,
    val sourceId: String,
    val rawText: String,
    val kind: String,
    val action: String,
    val lineNo: Int,
    val hitCount: Long,
    val order: Int,
    val modifierText: String?,
)

@Entity(tableName = "scripts")
data class ScriptEntity(
    @PrimaryKey val id: String,
    val name: String,
    val content: String,
    val hooksJson: String,
    val argument: String?,
    val enabled: Boolean,
    val order: Int,
    val lastRunStatus: String?,
    val lastRunAt: Long,
)

@Entity(tableName = "rewrites")
data class RewriteEntity(
    @PrimaryKey val id: String,
    val match: String,
    val actionType: String,
    val actionValue: String?,
    val enabled: Boolean,
)

@Entity(tableName = "splash_rules")
data class SplashRuleEntity(
    @PrimaryKey val id: String,
    val packageName: String,
    val matchType: String,
    val matchValue: String,
    val action: String,
    val delayMs: Int,
    val cooldownMs: Int,
    val enabled: Boolean,
    val hitCount: Long,
)

@Entity(tableName = "notify_rules")
data class NotifyRuleEntity(
    @PrimaryKey val id: String,
    val packageName: String?,
    val keyword: String?,
    val regex: String?,
    val action: String,
    val enabled: Boolean,
)

@Entity(
    tableName = "traffic_stats",
    indices = [Index("ts"), Index("packageName")],
)
data class TrafficStatEntity(
    @PrimaryKey(autoGenerate = true) val rowId: Long = 0,
    val ts: Long,
    val appUid: Int,
    val packageName: String?,
    val host: String?,
    val ruleId: String?,
    val chain: String?,
    val upBytes: Long,
    val downBytes: Long,
    val durationMs: Long,
)

@Entity(tableName = "installed_manifest")
data class InstalledManifestEntity(
    @PrimaryKey val entryId: String,
    val kind: String,
    val target: String,
    val source: String,
    val restorable: Boolean,
    val createdAt: Long,
)
