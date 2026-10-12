package io.github.vipsvip.syfuse.privileged

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

/**
 * 特权层装配。
 *
 * [LibsuPrivilegeProbe] 在无 root 设备上也能安全运行：
 * libsu 的 `Shell.getShell()` 会退化为普通 shell，`isRoot` 为 false，
 * 于是所有能力位都为 false——这正是"无特权设备功能不缺失"（14 的特权维度）的含义。
 */
@Module
@InstallIn(SingletonComponent::class)
abstract class PrivilegedModule {

    @Binds
    @Singleton
    abstract fun bindPrivilegeProbe(impl: LibsuPrivilegeProbe): PrivilegeProbe
}
