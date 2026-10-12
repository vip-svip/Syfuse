package io.github.vipsvip.syfuse.engine

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.github.vipsvip.syfuse.domain.engine.DataPlane
import io.github.vipsvip.syfuse.domain.engine.EngineController
import javax.inject.Singleton

/**
 * 引擎依赖装配。
 *
 * 内核替换点只有这一处：把 [NotLinkedDataPlane] 换成真实内核实现即可，
 * UI 与状态机完全不需要改动（9.2 的"内核可替换"由此得到保证）。
 */
@Module
@InstallIn(SingletonComponent::class)
object EngineModule {

    @Provides
    @Singleton
    fun provideDataPlane(impl: NotLinkedDataPlane): DataPlane = impl

    @Provides
    @Singleton
    fun provideEngineController(dataPlane: DataPlane): EngineController = EngineController(dataPlane)
}
