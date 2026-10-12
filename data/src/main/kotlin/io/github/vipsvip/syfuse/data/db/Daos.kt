package io.github.vipsvip.syfuse.data.db

import androidx.room.Dao
import androidx.room.Query
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

/**
 * DAO 集合。
 *
 * 约定：所有读取返回 Flow（UI 自动跟随数据变化），所有写入为 suspend（由调用方决定调度器）。
 * 危险操作（清空全部规则、重置统计）一律有独立方法名，便于在代码评审与静态检查中识别。
 */

@Dao
interface ProfileDao {
    @Query("SELECT * FROM profiles ORDER BY updatedAt DESC")
    fun observeAll(): Flow<List<ProfileEntity>>

    @Query("SELECT * FROM profiles WHERE id = :id")
    suspend fun findById(id: String): ProfileEntity?

    @Upsert
    suspend fun upsert(entity: ProfileEntity)

    @Query("DELETE FROM profiles WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface ProxyNodeDao {
    @Query("SELECT * FROM proxy_nodes ORDER BY sortIndex ASC, name ASC")
    fun observeAll(): Flow<List<ProxyNodeEntity>>

    @Query("SELECT * FROM proxy_nodes WHERE name = :name LIMIT 1")
    suspend fun findByName(name: String): ProxyNodeEntity?

    @Upsert
    suspend fun upsertAll(entities: List<ProxyNodeEntity>)

    @Query("UPDATE proxy_nodes SET latencyMs = :latencyMs WHERE id = :id")
    suspend fun updateLatency(id: String, latencyMs: Int?)

    @Query("DELETE FROM proxy_nodes WHERE id = :id")
    suspend fun delete(id: String)

    /** 危险操作：清空全部节点。调用方必须已获得用户二次确认。 */
    @Query("DELETE FROM proxy_nodes")
    suspend fun deleteAll()
}

@Dao
interface ProxyGroupDao {
    @Query("SELECT * FROM proxy_groups ORDER BY name ASC")
    fun observeAll(): Flow<List<ProxyGroupEntity>>

    @Upsert
    suspend fun upsertAll(entities: List<ProxyGroupEntity>)

    @Query("DELETE FROM proxy_groups WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface RuleSetDao {
    @Query("SELECT * FROM rule_sets ORDER BY name ASC")
    fun observeAll(): Flow<List<RuleSetEntity>>

    @Upsert
    suspend fun upsert(entity: RuleSetEntity)

    @Query("DELETE FROM rule_sets WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface RuleEntryDao {
    @Query("SELECT * FROM rule_entries WHERE sourceId = :sourceId ORDER BY `order` ASC, lineNo ASC")
    fun observeBySource(sourceId: String): Flow<List<RuleEntryEntity>>

    @Query("SELECT * FROM rule_entries ORDER BY sourceId ASC, lineNo ASC")
    fun observeAll(): Flow<List<RuleEntryEntity>>

    @Query("SELECT COUNT(*) FROM rule_entries")
    suspend fun count(): Int

    @Upsert
    suspend fun upsertAll(entities: List<RuleEntryEntity>)

    @Query("DELETE FROM rule_entries WHERE sourceId = :sourceId")
    suspend fun deleteBySource(sourceId: String)

    /** 危险操作：清空全部规则。 */
    @Query("DELETE FROM rule_entries")
    suspend fun deleteAll()
}

@Dao
interface ScriptDao {
    @Query("SELECT * FROM scripts ORDER BY `order` ASC")
    fun observeAll(): Flow<List<ScriptEntity>>

    @Upsert
    suspend fun upsert(entity: ScriptEntity)

    @Query("DELETE FROM scripts WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface RewriteDao {
    @Query("SELECT * FROM rewrites")
    fun observeAll(): Flow<List<RewriteEntity>>

    @Upsert
    suspend fun upsertAll(entities: List<RewriteEntity>)

    @Query("DELETE FROM rewrites WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface SplashRuleDao {
    @Query("SELECT * FROM splash_rules ORDER BY packageName ASC")
    fun observeAll(): Flow<List<SplashRuleEntity>>

    @Upsert
    suspend fun upsert(entity: SplashRuleEntity)

    @Query("DELETE FROM splash_rules WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface NotifyRuleDao {
    @Query("SELECT * FROM notify_rules")
    fun observeAll(): Flow<List<NotifyRuleEntity>>

    @Upsert
    suspend fun upsert(entity: NotifyRuleEntity)

    @Query("DELETE FROM notify_rules WHERE id = :id")
    suspend fun delete(id: String)
}

@Dao
interface TrafficStatDao {
    @Query("SELECT * FROM traffic_stats WHERE ts >= :since ORDER BY ts DESC LIMIT :limit")
    fun observeSince(since: Long, limit: Int): Flow<List<TrafficStatEntity>>

    @Query("SELECT SUM(upBytes) FROM traffic_stats WHERE ts >= :since")
    suspend fun sumUpBytes(since: Long): Long?

    @Query("SELECT SUM(downBytes) FROM traffic_stats WHERE ts >= :since")
    suspend fun sumDownBytes(since: Long): Long?

    @Query("DELETE FROM traffic_stats WHERE ts < :before")
    suspend fun purgeBefore(before: Long): Int

    @Upsert
    suspend fun insertAll(entities: List<TrafficStatEntity>)

    /** 危险操作：重置全部统计。 */
    @Query("DELETE FROM traffic_stats")
    suspend fun deleteAll()
}

@Dao
interface InstalledManifestDao {
    @Query("SELECT * FROM installed_manifest ORDER BY createdAt DESC")
    fun observeAll(): Flow<List<InstalledManifestEntity>>

    @Upsert
    suspend fun upsert(entity: InstalledManifestEntity)

    @Query("DELETE FROM installed_manifest WHERE entryId = :entryId")
    suspend fun delete(entryId: String)
}
