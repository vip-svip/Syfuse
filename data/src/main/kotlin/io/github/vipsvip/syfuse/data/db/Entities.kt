package io.github.vipsvip.syfuse.data.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/**
 * Room 实体定义。
 *
 * 字段与提示词第 10 部分的领域模型一一对应。刻意与领域模型分开而非直接用领域类注解，
 * 理由：领域模型要保证「内核无关 + 可序列化 round-trip」，而 Room 实体需要
 * 索引、列名与外部表结构，两者演进节奏不同。转换在 Mapper 层完成（M3 添加）。
 */

@Entity(tableName = "profiles")
data class ProfileEntity(
    @PrimaryKey val id: String,
    val name: String,
    val type: String,
    @ColumnInfo(name = "source_url") val sourceUrl: String?,
    @ColumnInfo(name = "update_interval_hours") val updateIntervalHours: Int?,
    @ColumnInfo(name = "upload_bytes") val uploadBytes: Long = 0,
    @ColumnInfo(name = "download_bytes") val downloadBytes: Long = 0,
    @ColumnInfo(name = "total_bytes") val totalBytes: Long = 0,
    @ColumnInfo(name = "expire_at") val expireAtMillis: Long?,
    val routingMode: String,
    @ColumnInfo(name = "created_at") val createdAtMillis: Long,
    @ColumnInfo(name = "updated_at") val updatedAtMillis: Long,
    @ColumnInfo(name = "schema_version") val schemaVersion: Int,
)

@Entity(
    tableName = "proxy_nodes",
    indices = [Index("profile_id"), Index("sort_index")],
)
data class ProxyNodeEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "profile_id") val profileId: String,
    val name: String,
    val protocol: String,
    val server: String,
    val port: Int,
    /** 凭据密文。加密与解密在 Repository 层完成，数据库内绝不存明文（红线条 R5）。 */
    @ColumnInfo(name = "auth_cipher") val authCipher: String,
    @ColumnInfo(name = "tls_json") val tlsJson: String?,
    @ColumnInfo(name = "transport_json") val transportJson: String?,
    val plugin: String?,
    val tags: String?,
    @ColumnInfo(name = "latency_ms") val latencyMillis: Long?,
    val favorite: Boolean = false,
    @ColumnInfo(name = "sort_index") val sortIndex: Int = 0,
    @ColumnInfo(name = "raw_json") val rawJson: String?,
)

@Entity(
    tableName = "proxy_groups",
    indices = [Index("profile_id")],
)
data class ProxyGroupEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "profile_id") val profileId: String,
    val name: String,
    val type: String,
    val members: String?,
    val url: String?,
    @ColumnInfo(name = "test_interval_seconds") val testIntervalSeconds: Int?,
    @ColumnInfo(name = "tolerance_ms") val toleranceMillis: Int?,
    val nested: Boolean = false,
)

@Entity(tableName = "rule_sets")
data class RuleSetEntity(
    @PrimaryKey val id: String,
    val name: String,
    @ColumnInfo(name = "source_url") val sourceUrl: String?,
    @ColumnInfo(name = "local_path") val localPath: String?,
    val format: String,
    @ColumnInfo(name = "update_interval_hours") val updateIntervalHours: Int?,
    @ColumnInfo(name = "rule_count") val ruleCount: Int = 0,
    @ColumnInfo(name = "last_updated") val lastUpdatedMillis: Long?,
    val hash: String?,
    val enabled: Boolean = true,
)

@Entity(
    tableName = "rule_entries",
    indices = [Index("source_id"), Index("order")],
)
data class RuleEntryEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "source_id") val sourceId: String,
    @ColumnInfo(name = "raw_text") val rawText: String,
    val kind: String,
    val action: String,
    val modifiers: String?,
    @ColumnInfo(name = "line_no") val lineNo: Int = 0,
    val scope: String?,
    @ColumnInfo(name = "hit_count") val hitCount: Long = 0,
    val order: Int = 0,
)

@Entity(tableName = "scripts")
data class ScriptEntity(
    @PrimaryKey val id: String,
    val name: String,
    val content: String,
    val hooks: String?,
    val argument: String?,
    val enabled: Boolean = true,
    val order: Int = 0,
    @ColumnInfo(name = "last_run_status") val lastRunStatus: String?,
    @ColumnInfo(name = "last_run_at") val lastRunAtMillis: Long?,
)

@Entity(tableName = "rewrites")
data class RewriteEntity(
    @PrimaryKey val id: String,
    val match: String,
    @ColumnInfo(name = "action_type") val actionType: String,
    @ColumnInfo(name = "action_value") val actionValue: String?,
    val enabled: Boolean = true,
)

@Entity(tableName = "filter_configs")
data class FilterConfigEntity(
    @PrimaryKey val id: String,
    val name: String,
    val sources: String?,
    val enabled: Boolean = true,
    @ColumnInfo(name = "update_interval_hours") val updateIntervalHours: Int?,
    val priority: Int = 0,
)

@Entity(tableName = "splash_skip_rules")
data class SplashSkipRuleEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "package_name") val packageName: String,
    @ColumnInfo(name = "match_type") val matchType: String,
    @ColumnInfo(name = "match_value") val matchValue: String,
    val action: String,
    @ColumnInfo(name = "delay_ms") val delayMillis: Long = 500,
    @ColumnInfo(name = "cooldown_ms") val cooldownMillis: Long = 3000,
    val enabled: Boolean = false,
    @ColumnInfo(name = "hit_count") val hitCount: Long = 0,
)

@Entity(tableName = "notify_rules")
data class NotifyRuleEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "package_name") val packageName: String?,
    val keyword: String?,
    val regex: String?,
    val action: String,
    val enabled: Boolean = false,
)

/**
 * 已安装的系统级改动清单（提示词 6.9 侵入原则）。
 *
 * 这张表是红线条 R6「可完整还原」的落地载体：卸载流程按表逐项还原，
 * 表中任一项 restorable=false 即代表存在无法还原的改动，必须阻断该类操作。
 */
@Entity(tableName = "installed_manifest")
data class InstalledManifestEntity(
    @PrimaryKey @ColumnInfo(name = "entry_id") val entryId: String,
    val kind: String,
    val target: String,
    val source: String,
    val restorable: Boolean,
    @ColumnInfo(name = "created_at") val createdAtMillis: Long,
)

/**
 * 流量统计。
 *
 * 按时间分区存储，用于排行与图表。数据保留期由设置页控制（提示词 5.10）。
 */
@Entity(
    tableName = "traffic_stats",
    indices = [Index("ts"), Index("package_name")],
)
data class TrafficStatEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "ts") val tsMillis: Long,
    @ColumnInfo(name = "app_uid") val appUid: Int,
    @ColumnInfo(name = "package_name") val packageName: String?,
    val host: String,
    @ColumnInfo(name = "rule_id") val ruleId: String?,
    val chain: String?,
    @ColumnInfo(name = "up_bytes") val upBytes: Long,
    @ColumnInfo(name = "down_bytes") val downBytes: Long,
    @ColumnInfo(name = "duration_ms") val durationMillis: Long,
)

@Entity(tableName = "conn_snapshots")
data class ConnSnapshotEntity(
    @PrimaryKey val id: String,
    val network: String,
    val type: String,
    @ColumnInfo(name = "src_app") val srcApp: String?,
    @ColumnInfo(name = "dst_host") val dstHost: String,
    @ColumnInfo(name = "dst_ip") val dstIp: String?,
    @ColumnInfo(name = "rule_text") val ruleText: String,
    val chain: String?,
    val up: Long,
    val down: Long,
    @ColumnInfo(name = "start_at") val startAtMillis: Long,
    val state: String,
)
