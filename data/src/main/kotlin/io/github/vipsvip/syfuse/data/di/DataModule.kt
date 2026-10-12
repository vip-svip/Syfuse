package io.github.vipsvip.syfuse.data.di

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.core.DataStoreFactory
import androidx.room.Room
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.github.vipsvip.syfuse.data.db.SyfuseDatabase
import io.github.vipsvip.syfuse.data.security.AndroidKeystoreCredentialVault
import io.github.vipsvip.syfuse.data.security.CredentialVault
import io.github.vipsvip.syfuse.data.settings.AppSettingsDataStore
import io.github.vipsvip.syfuse.data.settings.AppSettingsSerializer
import io.github.vipsvip.syfuse.domain.config.ConfigBundleCodec
import io.github.vipsvip.syfuse.domain.settings.AppSettings
import java.io.File
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object DataModule {

    @Provides
    @Singleton
    fun provideDatabase(@ApplicationContext context: Context): SyfuseDatabase =
        Room.databaseBuilder(context, SyfuseDatabase::class.java, SyfuseDatabase.NAME)
            // 迁移策略：本版本为 v1，尚无历史版本需要迁移。
            // 有意不使用 fallbackToDestructiveMigration——它会静默清空用户数据，
            // 与第 10 部分"导入旧版本自动迁移并备份原文件"的要求冲突。
            .build()

    @Provides
    fun provideProfileDao(db: SyfuseDatabase) = db.profileDao()

    @Provides
    fun provideProxyNodeDao(db: SyfuseDatabase) = db.proxyNodeDao()

    @Provides
    fun provideProxyGroupDao(db: SyfuseDatabase) = db.proxyGroupDao()

    @Provides
    fun provideRuleSetDao(db: SyfuseDatabase) = db.ruleSetDao()

    @Provides
    fun provideRuleEntryDao(db: SyfuseDatabase) = db.ruleEntryDao()

    @Provides
    fun provideScriptDao(db: SyfuseDatabase) = db.scriptDao()

    @Provides
    fun provideRewriteDao(db: SyfuseDatabase) = db.rewriteDao()

    @Provides
    fun provideSplashRuleDao(db: SyfuseDatabase) = db.splashRuleDao()

    @Provides
    fun provideNotifyRuleDao(db: SyfuseDatabase) = db.notifyRuleDao()

    @Provides
    fun provideTrafficStatDao(db: SyfuseDatabase) = db.trafficStatDao()

    @Provides
    fun provideInstalledManifestDao(db: SyfuseDatabase) = db.installedManifestDao()

    @Provides
    @Singleton
    fun provideCredentialVault(): CredentialVault = AndroidKeystoreCredentialVault()

    @Provides
    @Singleton
    fun provideSettingsDataStore(
        @ApplicationContext context: Context,
    ): DataStore<AppSettings> = DataStoreFactory.create(
        serializer = AppSettingsSerializer,
        produceFile = { File(context.filesDir, "syfuse-settings.json") },
    )

    @Provides
    @Singleton
    fun provideAppSettingsDataStore(store: DataStore<AppSettings>): AppSettingsDataStore =
        AppSettingsDataStore(store)

    @Provides
    @Singleton
    fun provideConfigBundleCodec(): ConfigBundleCodec = ConfigBundleCodec()
}
