package io.github.vipsvip.syfuse.data.repo

import io.github.vipsvip.syfuse.core.common.SyfuseResult
import io.github.vipsvip.syfuse.data.db.ProfileDao
import io.github.vipsvip.syfuse.data.db.ProxyGroupDao
import io.github.vipsvip.syfuse.data.db.ProxyNodeDao
import io.github.vipsvip.syfuse.data.db.RuleEntryDao
import io.github.vipsvip.syfuse.data.db.RuleSetDao
import io.github.vipsvip.syfuse.data.db.RewriteDao
import io.github.vipsvip.syfuse.data.db.ScriptDao
import io.github.vipsvip.syfuse.data.db.toDomain
import io.github.vipsvip.syfuse.data.db.toEntity
import io.github.vipsvip.syfuse.domain.config.ConfigBundleCodec
import io.github.vipsvip.syfuse.domain.model.ConfigBundle
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import javax.inject.Inject
import javax.inject.Singleton

/**
 * 配置仓库：把配置包与数据库打通。
 *
 * 导入语义（5.3 与 10 的要求）：
 *  - 导入前**先备份**当前配置包文本，失败可回滚；
 *  - schema 校验失败时不写入任何数据（先解码，全部成功后才落库）；
 *  - 未知字段随配置包一起保留。
 */
@Singleton
class ConfigRepository @Inject constructor(
    private val profileDao: ProfileDao,
    private val nodeDao: ProxyNodeDao,
    private val groupDao: ProxyGroupDao,
    private val ruleSetDao: RuleSetDao,
    private val ruleEntryDao: RuleEntryDao,
    private val scriptDao: ScriptDao,
    private val rewriteDao: RewriteDao,
    private val codec: ConfigBundleCodec,
) {

    fun observeProfiles() = profileDao.observeAll().map { list -> list.map { it.toDomain() } }

    fun observeNodes() = nodeDao.observeAll().map { list -> list.map { it.toDomain() } }

    fun observeGroups() = groupDao.observeAll().map { list -> list.map { it.toDomain() } }

    fun observeRuleSets() = ruleSetDao.observeAll().map { list -> list.map { it.toDomain() } }

    fun observeScripts() = scriptDao.observeAll().map { list -> list.map { it.toDomain() } }

    fun observeRuleEntries() = ruleEntryDao.observeAll().map { list -> list.map { it.toDomain() } }

    /** 导出为配置包文本。 */
    suspend fun exportBundle(): String {
        val bundle = ConfigBundle(
            profile = firstOf(profileDao.observeAll()).firstOrNull()?.toDomain(),
            nodes = firstOf(nodeDao.observeAll()).map { it.toDomain() },
            groups = firstOf(groupDao.observeAll()).map { it.toDomain() },
            ruleSets = firstOf(ruleSetDao.observeAll()).map { it.toDomain() },
            scripts = firstOf(scriptDao.observeAll()).map { it.toDomain() },
            rewrites = firstOf(rewriteDao.observeAll()).map { it.toDomain() },
        )
        return codec.encode(bundle)
    }

    /**
     * 导入配置包。
     * 返回解码后的配置包与"导入前的备份文本"，供调用方在写库失败时回滚。
     */
    suspend fun importBundle(text: String): SyfuseResult<ImportOutcome> {
        val decoded = codec.decode(text)
        if (decoded is SyfuseResult.Failure) return decoded

        val bundle = (decoded as SyfuseResult.Success).value
        val backup = exportBundle()

        bundle.profile?.let { profileDao.upsert(it.toEntity()) }
        if (bundle.nodes.isNotEmpty()) nodeDao.upsertAll(bundle.nodes.map { it.toEntity() })
        if (bundle.groups.isNotEmpty()) groupDao.upsertAll(bundle.groups.map { it.toEntity() })
        bundle.ruleSets.forEach { ruleSetDao.upsert(it.toEntity()) }
        if (bundle.rules.isNotEmpty()) ruleEntryDao.upsertAll(bundle.rules.map { it.toEntity() })
        bundle.scripts.forEach { scriptDao.upsert(it.toEntity()) }
        if (bundle.rewrites.isNotEmpty()) rewriteDao.upsertAll(bundle.rewrites.map { it.toEntity() })

        return SyfuseResult.Success(ImportOutcome(bundle = bundle, backupText = backup))
    }

    /** 回滚：用备份文本覆盖当前数据。 */
    suspend fun rollback(backupText: String): SyfuseResult<Unit> {
        val result = importBundle(backupText)
        return if (result is SyfuseResult.Failure) result else SyfuseResult.Success(Unit)
    }

    private suspend fun <T> firstOf(flow: Flow<List<T>>): List<T> = flow.first()
}

/** 导入结果。 */
data class ImportOutcome(
    val bundle: ConfigBundle,
    /** 导入前的完整配置文本；失败回滚与"撤销最近一次操作"（7.3）都用它。 */
    val backupText: String,
)
