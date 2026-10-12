package io.github.vipsvip.syfuse.privileged

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.domain.model.RootProvider
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Test

/**
 * root 提供方探测逻辑的单测。
 *
 * 为什么要单测「哪个文件存在」这种看似简单的逻辑：
 * 探测顺序有真实后果 —— 部分 KernelSU 设备保留了 Magisk 残留目录，
 * 若先判 Magisk 会误报，导致能力探测页给出错误的提权建议。
 * 这个顺序约束必须被测试锁住，否则将来重构极易被无意改掉。
 */
class FileSystemPrivilegeProbeTest {

    /** 构造指定路径集合的假文件系统。 */
    private fun fakeFs(vararg existingPaths: String) = object : FileSystemView {
        private val paths = existingPaths.toSet()
        override fun exists(path: String) = path in paths
    }

    @Test
    fun `无任何特征路径时判定为无 root`() = runTest {
        val probe = FileSystemPrivilegeProbe(fakeFs())
        val state = probe.probe()

        assertThat(state.rootProvider).isEqualTo(RootProvider.NONE)
        assertThat(state.rootGranted).isFalse()
    }

    @Test
    fun `检测到 KernelSU 特征路径`() = runTest {
        val probe = FileSystemPrivilegeProbe(fakeFs("/data/adb/ksu"))
        assertThat(probe.probe().rootProvider).isEqualTo(RootProvider.KERNELSU)
    }

    @Test
    fun `检测到 APatch 特征路径`() = runTest {
        val probe = FileSystemPrivilegeProbe(fakeFs("/data/adb/ap"))
        assertThat(probe.probe().rootProvider).isEqualTo(RootProvider.APATCH)
    }

    @Test
    fun `检测到 Magisk 特征路径`() = runTest {
        val probe = FileSystemPrivilegeProbe(fakeFs("/data/adb/magisk"))
        assertThat(probe.probe().rootProvider).isEqualTo(RootProvider.MAGISK)
    }

    @Test
    fun `KernelSU 与 Magisk 残留共存时优先判定 KernelSU`() = runTest {
        // 这是本测试类的核心用例：顺序错误会导致误报
        val probe = FileSystemPrivilegeProbe(
            fakeFs("/data/adb/magisk", "/data/adb/ksu"),
        )
        assertThat(probe.probe().rootProvider).isEqualTo(RootProvider.KERNELSU)
    }

    @Test
    fun `探测结果不谎报 rootGranted`() = runTest {
        // 仅凭文件存在无法确认提权是否被授权，必须保持 false 而非猜测为 true
        val probe = FileSystemPrivilegeProbe(fakeFs("/data/adb/ksu"))
        assertThat(probe.probe().rootGranted).isFalse()
    }
}
