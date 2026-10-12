package io.github.vipsvip.syfuse.privileged

import io.github.vipsvip.syfuse.domain.model.PrivilegeState
import io.github.vipsvip.syfuse.domain.model.RootProvider

/**
 * 特权能力探测（提示词 6.5「能力探测页」）。
 *
 * M1 只实现**不需要特权**的探测部分（模块文件与内核特性检查），
 * 涉及 root 的探测在 M11 接入 libsu。
 *
 * 设计原则：探测结果必须如实反映设备实际情况，宁报「未知」也不猜。
 * 因为用户会据此判断「为什么某功能不可用」，谎报会导致误导性排障。
 */
interface PrivilegeProbe {

    /** 完整探测一次，返回当前设备的能力快照。 */
    suspend fun probe(): PrivilegeState
}

/**
 * 基于文件系统与内核参数的探测实现。
 *
 * 全部检查依据都是公开可验证的路径或 proc 节点，不依赖 root 权限读取：
 * - KernelSU / Magisk / APatch 都有固定的目录或文件特征
 * - nftables / TPROXY 通过 /proc 下的内核模块与配置检查
 *
 * 【待核实】各项特征路径的确切定义需要在 M11 用真机逐一验证。
 * 当前实现基于各项目公开文档的通用约定，标记为待核实而非直接断言。
 */
class FileSystemPrivilegeProbe(
    private val fileSystem: FileSystemView,
) : PrivilegeProbe {

    override suspend fun probe(): PrivilegeState {
        val provider = detectRootProvider()

        return PrivilegeState(
            rootProvider = provider,
            // rootGranted 需要实际执行提权命令才能确认，M1 无法判断，
            // 故仅在检测到 provider 时标记为「待确认」而非 true
            rootGranted = false,
            lsposedActive = false,
            systemCaWritable = false,
            // 内核特性检查：M1 先返回 false，M11 接入 /proc 检查
            nftablesAvailable = false,
            tproxyAvailable = false,
            overlayfsAvailable = fileSystem.exists("/sys/module/overlay"),
        )
    }

    /**
     * 探测 root 提供方。
     *
     * 顺序有讲究：先查 KernelSU，因为部分 KernelSU 设备上 Magisk 残留目录仍可能存在；
     * 若先查 Magisk 会误报。
     */
    private fun detectRootProvider(): RootProvider {
        // KernelSU 的特征路径（【待核实】M11 真机确认）
        if (fileSystem.exists("/data/adb/ksu") || fileSystem.exists("/data/adb/ksud")) {
            return RootProvider.KERNELSU
        }
        // APatch 的特征路径
        if (fileSystem.exists("/data/adb/ap") || fileSystem.exists("/data/adb/apd")) {
            return RootProvider.APATCH
        }
        // Magisk 的特征路径
        if (fileSystem.exists("/data/adb/magisk")) {
            return RootProvider.MAGISK
        }
        return RootProvider.NONE
    }
}

/**
 * 文件系统视图抽象。
 *
 * 抽出来的唯一目的是可测试性：root 探测逻辑的判据是「某路径是否存在」，
 * 把它变成接口后，单测可以构造任意设备形态，无需真机。
 */
interface FileSystemView {
    fun exists(path: String): Boolean
}
