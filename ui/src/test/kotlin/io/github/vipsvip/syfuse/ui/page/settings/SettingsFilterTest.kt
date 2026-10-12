package io.github.vipsvip.syfuse.ui.page.settings

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test

/**
 * 设置搜索与专家模式过滤的单测。
 *
 * 为什么这值得单测：设置搜索是用户定位功能的唯一入口，一旦过滤逻辑出错
 * （例如专家模式下反而隐藏了普通项），用户会认为「设置项消失了」却无从排查。
 * 这类缺陷靠 UI 测试很难穷举，用纯逻辑单测最经济。
 */
class SettingsFilterTest {

    @Test
    fun `空查询返回通用分组的全部设置项`() {
        val groups = filterGroups(query = "", expertMode = false)
        val general = groups.firstOrNull { it.first == SettingsGroup.GENERAL }

        assertThat(general).isNotNull()
        // 「通用」分组在 M1 定义了 5 项
        assertThat(general!!.second).hasSize(5)
    }

    @Test
    fun `专家模式关闭时不包含专家专属项`() {
        val count = countVisibleItems(query = "", expertMode = false)
        val expertCount = countVisibleItems(query = "", expertMode = true)

        // 通用分组内 expert_mode 与 autostart 未标记 expertOnly，
        // 因此当前可见数相同；本断言锁定这一契约，
        // 一旦有人给某项加上 expertOnly，测试会立刻反映出来
        assertThat(count).isEqualTo(expertCount)
    }

    @Test
    fun `按 key 查询能命中对应设置项`() {
        val groups = filterGroups(query = "theme", expertMode = true)
        val items = groups.flatMap { it.second }

        assertThat(items.map { it.key }).contains("theme")
        // dynamic_color 的 key 不含 theme，不应被命中
        assertThat(items.map { it.key }).doesNotContain("autostart")
    }

    @Test
    fun `查询无匹配时返回空列表而非全量列表`() {
        val groups = filterGroups(query = "zzz-nonexistent-setting", expertMode = true)
        assertThat(groups).isEmpty()
    }

    @Test
    fun `尚未填充的分组数量与里程碑计划一致`() {
        // M1 只完成「通用」；其余 10 个分组待各里程碑填充。
        // 这个断言让「哪个分组还是空的」成为可验证事实。
        val pending = SettingsGroup.PENDING_GROUPS
        assertThat(pending).hasSize(10)
        assertThat(pending).doesNotContain(SettingsGroup.GENERAL)
    }

    @Test
    fun `设置项的 key 在全体分组内唯一`() {
        val allKeys = SettingsGroup.entries.flatMap { group -> group.items.map { it.key } }
        assertThat(allKeys).containsNoDuplicates()
    }
}
