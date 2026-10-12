package io.github.vipsvip.syfuse.data.db

import androidx.room.Database
import androidx.room.RoomDatabase
import androidx.room.TypeConverter

/**
 * 应用数据库。
 *
 * 版本策略（提示词 10 「兼容性要求」）：
 * - [version] 与 [io.github.vipsvip.syfuse.core.SyfuseConstants.SCHEMA_VERSION] 同步递增
 * - 每次升级必须提供 Migration，禁止 fallbackToDestructiveMigration
 *   （那会静默丢掉用户节点与规则，属数据丢失）
 *
 * M1 只建立实体与数据库骨架，DAO 在 M3（节点）与 M4（规则）填充。
 */
@Database(
    entities = [
        ProfileEntity::class,
        ProxyNodeEntity::class,
        ProxyGroupEntity::class,
        RuleSetEntity::class,
        RuleEntryEntity::class,
        ScriptEntity::class,
        RewriteEntity::class,
        FilterConfigEntity::class,
        SplashSkipRuleEntity::class,
        NotifyRuleEntity::class,
        InstalledManifestEntity::class,
        TrafficStatEntity::class,
        ConnSnapshotEntity::class,
    ],
    version = 1,
    exportSchema = true,
)
abstract class SyfuseDatabase : RoomDatabase() {
    // DAO 访问器在 M3 / M4 添加。此处留空是有意的：
    // 数据库与迁移基础设施先落地，业务表按里程碑增量接入。
}

/**
 * Room 类型转换器。
 *
 * 把 List / Map 以 JSON 字符串存储。取舍：牺牲 SQL 层的可查询性，换取 schema 演进自由度。
 * 需要按 tag 或 member 查询时，改为独立关联表；当前这些字段只做整体读写。
 */
class Converters {

    @TypeConverter
    fun fromStringList(value: List<String>?): String? = value?.joinToString(SEPARATOR)

    @TypeConverter
    fun toStringList(value: String?): List<String>? =
        value?.takeIf { it.isNotEmpty() }?.split(SEPARATOR)

    @TypeConverter
    fun fromStringMap(value: Map<String, String>?): String? =
        value?.entries?.joinToString(SEPARATOR) { "${it.key}=${it.value}" }

    @TypeConverter
    fun toStringMap(value: String?): Map<String, String>? =
        value?.takeIf { it.isNotEmpty() }
            ?.split(SEPARATOR)
            ?.mapNotNull { entry ->
                // 只按第一个 = 切分，允许值里含 =
                val idx = entry.indexOf('=')
                if (idx <= 0) null else entry.substring(0, idx) to entry.substring(idx + 1)
            }
            ?.toMap()

    private companion object {
        /**
         * 用不可见字符作分隔符。
         * 理由：域名、tag、正则里都可能出现逗号、竖线、分号，用常见字符会导致数据损坏。
         * \u0000 在 Android 文件名与命令行里不会出现，是安全选择。
         */
        const val SEPARATOR = "\u0000"
    }
}
