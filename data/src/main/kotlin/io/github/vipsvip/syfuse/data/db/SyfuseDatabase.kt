package io.github.vipsvip.syfuse.data.db

import androidx.room.Database
import androidx.room.RoomDatabase

/**
 * 应用数据库。
 *
 * `exportSchema = true` 配合 build.gradle.kts 里的 room.schemaLocation，
 * 使每次 schema 变更都产出可审阅的 JSON。这是第 10 部分"版本迁移 + 原文件备份"
 * 能够被验证的前提——没有 schema 快照，迁移测试无从谈起。
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
        SplashRuleEntity::class,
        NotifyRuleEntity::class,
        TrafficStatEntity::class,
        InstalledManifestEntity::class,
    ],
    version = 1,
    exportSchema = true,
)
abstract class SyfuseDatabase : RoomDatabase() {
    abstract fun profileDao(): ProfileDao
    abstract fun proxyNodeDao(): ProxyNodeDao
    abstract fun proxyGroupDao(): ProxyGroupDao
    abstract fun ruleSetDao(): RuleSetDao
    abstract fun ruleEntryDao(): RuleEntryDao
    abstract fun scriptDao(): ScriptDao
    abstract fun rewriteDao(): RewriteDao
    abstract fun splashRuleDao(): SplashRuleDao
    abstract fun notifyRuleDao(): NotifyRuleDao
    abstract fun trafficStatDao(): TrafficStatDao
    abstract fun installedManifestDao(): InstalledManifestDao

    companion object {
        const val NAME: String = "syfuse.db"
    }
}
