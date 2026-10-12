package io.github.vipsvip.syfuse.privileged

import com.topjohnwu.superuser.Shell
import io.github.vipsvip.syfuse.domain.model.PrivilegeState
import io.github.vipsvip.syfuse.domain.model.RootProvider
import javax.inject.Inject
import javax.inject.Singleton

/** 能力探测。返回的每一项都必须是实测结果。 */
interface PrivilegeProbe {
    suspend fun probe(): PrivilegeState
}

/**
 * 基于 libsu 的真实探测实现。
 *
 * 设计要点：
 *  - 一次 `su` 会话内跑完所有检查，而不是每项各起一次 su（后者在部分设备上会反复弹授权框）；
 *  - 每项判定都基于**文件系统或内核的实际输出**，不做"有 root 就都可用"的推断；
 *  - 结果带探测时间戳，UI 必须显示，避免用户看过期结论（[PrivilegeState.probedAt]）。
 */
@Singleton
class LibsuPrivilegeProbe @Inject constructor() : PrivilegeProbe {

    override suspend fun probe(): PrivilegeState {
        val shell = runCatching { Shell.getShell() }.getOrNull()
            ?: return PrivilegeState(probedAt = System.currentTimeMillis())

        val rootGranted = shell.isRoot

        val paths = mapOf(
            "ksu" to "/data/adb/ksud",
            "ksu_dir" to "/data/adb/ksu",
            "magisk" to "/data/adb/magisk",
            "apatch" to "/data/adb/apd",
            "apatch_dir" to "/data/adb/ap",
            "lsposed" to "/data/adb/lspd",
            "module" to "/data/adb/modules/syfuse/module.prop",
            "cacerts" to "/system/etc/security/cacerts",
        )

        val probes = buildList {
            paths.forEach { (_, path) -> add(PrivilegeCommand.Exists(path)) }
            add(PrivilegeCommand.Writable(paths.getValue("cacerts")))
            add(PrivilegeCommand.Which("nft"))
            add(PrivilegeCommand.Filesystems)
            add(PrivilegeCommand.IptablesTargets)
            add(PrivilegeCommand.LoadedModules)
        }

        // 用带标记的输出把多次探测合并为一次调用：命令全部来自白名单枚举，
        // 参数已在构造阶段通过正则校验，因此这里不存在注入面。
        val script = buildString {
            probes.forEachIndexed { index, command ->
                val cmd = command.argv.joinToString(" ")
                append("( $cmd ) >/dev/null 2>&1 && echo __SYFUSE_$index=1 || echo __SYFUSE_$index=0\n")
            }
            append("cat /proc/filesystems 2>/dev/null | grep -q overlay && echo __SYFUSE_FS=1 || echo __SYFUSE_FS=0\n")
            append("cat /proc/net/ip_tables_targets 2>/dev/null | grep -q TPROXY && echo __SYFUSE_TP=1 || echo __SYFUSE_TP=0\n")
            append("cat /proc/modules 2>/dev/null | grep -q xt_TPROXY && echo __SYFUSE_TPM=1 || echo __SYFUSE_TPM=0\n")
        }

        val out = runCatching { Shell.cmd(script).exec().out }
            .getOrElse { emptyList() }

        val flags = out.mapNotNull { line ->
            val idx = line.indexOf('=')
            if (idx <= 0) null else line.substring(0, idx) to (line.substring(idx + 1).trim() == "1")
        }.toMap()

        fun flag(name: String): Boolean = flags["__SYFUSE_$name"] == true

        val provider = when {
            flag("0") || flag("1") -> RootProvider.KERNELSU
            flag("2") -> RootProvider.MAGISK
            flag("3") || flag("4") -> RootProvider.APATCH
            else -> RootProvider.NONE
        }

        return PrivilegeState(
            rootProvider = if (rootGranted) provider else RootProvider.NONE,
            rootGranted = rootGranted,
            lsposedActive = flag("5"),
            moduleInstalled = flag("6"),
            systemCaWritable = flag("7"),
            // nft 二进制存在只是必要条件；TPROXY 需要内核侧目标支持，两者都满足才算可用
            nftablesAvailable = flag("8") && (flag("TP") || flag("TPM")),
            tproxyAvailable = flag("TP") || flag("TPM"),
            overlayfsAvailable = flag("FS"),
            moduleScope = emptyList(),
            probedAt = System.currentTimeMillis(),
        )
    }
}
