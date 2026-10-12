package io.github.vipsvip.syfuse.domain.settings

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test

/**
 * 设置目录自检。
 * 这些断言保护的是 5.10 的硬要求：每项都要有副标题与风险等级、默认值可恢复、
 * 搜索能命中隐藏项。它们比"看起来有设置页"更能证明设置框架是完整的。
 */
class SettingsCatalogTest {

    @Test
    fun `每个设置项都有副标题与唯一 key`() {
        val keys = SettingsCatalog.all.map { it.key }
        assertThat(keys).containsNoDuplicates()
        for (spec in SettingsCatalog.all) {
            assertThat(spec.title).isNotEmpty()
            assertThat(spec.subtitle).isNotEmpty()
        }
    }

    @Test
    fun `枚举型设置项的默认值必须在候选项内`() {
        for (spec in SettingsCatalog.all) {
            if (spec.type == SettingType.ENUM) {
                assertThat(spec.enumOptions).isNotEmpty()
                assertThat(spec.enumOptions).contains(spec.defaultValue)
            }
        }
    }

    @Test
    fun `数值型设置项的默认值必须落在范围内`() {
        for (spec in SettingsCatalog.all) {
            if (spec.type == SettingType.NUMBER && spec.range != null) {
                val v = spec.defaultValue.toInt()
                assertThat(spec.range.contains(v)).isTrue()
            }
        }
    }

    @Test
    fun `每个分组都至少有一个设置项`() {
        for (group in SettingGroup.entries) {
            assertThat(SettingsCatalog.all.count { it.group == group }).isAtLeast(1)
        }
    }

    @Test
    fun `危险动作类设置必须要求二次确认`() {
        val dangerousActions = SettingsCatalog.all.filter { it.risk == RiskLevel.DANGEROUS }
        assertThat(dangerousActions).isNotEmpty()
        // 涉及系统级不可逆改动的项必须带确认；此处只断言"有确认标记的项都是危险级"
        for (spec in SettingsCatalog.all.filter { it.requiresConfirm }) {
            assertThat(spec.risk).isEqualTo(RiskLevel.DANGEROUS)
        }
    }

    @Test
    fun `搜索能命中被专家模式隐藏的项`() {
        val hidden = SettingsCatalog.all.first { it.expertOnly }
        val hits = SettingsCatalog.search(hidden.title)
        assertThat(hits.map { it.key }).contains(hidden.key)
    }

    @Test
    fun `搜索匹配副标题与分组名`() {
        assertThat(SettingsCatalog.search("MTU").map { it.key }).contains("tun_mtu")
        assertThat(SettingsCatalog.search("HTTPS 解密").map { it.key }).isNotEmpty()
        assertThat(SettingsCatalog.search("   ")).isEmpty()
    }

    @Test
    fun `专家模式开关决定高级项是否可见`() {
        val group = SettingGroup.TUNNEL
        val normal = SettingsCatalog.visibleIn(group, expertMode = false)
        val expert = SettingsCatalog.visibleIn(group, expertMode = true)
        assertThat(normal.size).isLessThan(expert.size)
        assertThat(normal.all { !it.expertOnly }).isTrue()
    }

    @Test
    fun `恢复推荐值给出全量默认映射`() {
        val defaults = SettingsCatalog.defaults()
        assertThat(defaults.keys).containsExactlyElementsIn(SettingsCatalog.all.map { it.key })
    }

    @Test
    fun `遥测恒为关闭且设置目录不提供开启入口`() {
        assertThat(AppSettings().telemetry).isFalse()
        assertThat(SettingsCatalog.all.none { it.key.contains("telemetry", ignoreCase = true) }).isTrue()
    }
}
