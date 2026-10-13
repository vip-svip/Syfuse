package io.github.vipsvip.syfuse.engine

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.github.vipsvip.syfuse.daemon.DaemonClient
import io.github.vipsvip.syfuse.daemon.DaemonProtocol
import io.github.vipsvip.syfuse.daemon.LocalSocketTransport
import io.github.vipsvip.syfuse.domain.engine.DataPlane
import io.github.vipsvip.syfuse.domain.engine.EngineController
import java.io.File
import javax.inject.Singleton

/**
 * 引擎依赖装配。
 *
 * 内核替换点只有这一处：换掉 [provideDataPlane] 返回的实现即可，
 * UI 与状态机完全不需要改动（9.2 的"内核可替换"由此得到保证）。
 *
 * 当前绑定 [UdsDataPlane]（走 UDS 连守护进程 syfused）。它在
 * "代理隧道尚未实现"时会如实失败并给出可展示的原因，不会让界面
 * 显示"已连接" —— 这一点由 `tests/run_module_checks.sh` 的 R2 段守着。
 */
@Module
@InstallIn(SingletonComponent::class)
object EngineModule {

    /**
     * 守护进程客户端。
     *
     * 令牌来源：先读模块生成的令牌文件。**注意这只是一个过渡方案** ——
     * `/data/adb/syfuse/` 是 0700 且属 root，无 root 的 App 通常读不到它，
     * 于是会走到 `DaemonClient` 的 "no_token" 分支并给出可操作提示
     * （而不是抛空指针）。App 与模块之间正式的令牌配对流程
     * （由模块侧授权、App 侧持久化）尚未实现，属后续里程碑。
     *
     * 传输必须在这里显式传入：`DaemonClient` 住在纯 JVM 的 `:daemon-protocol` 里，
     * 引用不到 `android.net.LocalSocket`，因此它**没有**默认传输实现。
     * 这是刻意的 —— 装配点只有一个，真机专属的实现就只在一个地方出现。
     * 副作用（也是收益）：纯 JVM 模块的单测永远不可能误用框架类，
     * "App → UDS → 守护进程"的往返因此能在沙箱里被真实验证。
     */
    @Provides
    @Singleton
    fun provideDaemonClient(): DaemonClient = DaemonClient(
        dataDir = DaemonProtocol.DATA_DIR,
        tokenProvider = { readTokenOrNull(DaemonProtocol.DATA_DIR) },
        transport = LocalSocketTransport(),
    )

    @Provides
    @Singleton
    fun provideDataPlane(impl: UdsDataPlane): DataPlane = impl

    @Provides
    @Singleton
    fun provideEngineController(dataPlane: DataPlane): EngineController = EngineController(dataPlane)
}

/** 读取访问令牌；读不到或为空时返回 null，由调用方转成可展示的错误。 */
private fun readTokenOrNull(dataDir: String): String? =
    runCatching { File(dataDir, DaemonProtocol.TOKEN_NAME).readText().trim() }
        .getOrNull()
        ?.takeIf { it.isNotEmpty() }
