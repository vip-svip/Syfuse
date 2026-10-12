package io.github.vipsvip.syfuse.ui.page.settings

import androidx.annotation.StringRes
import io.github.vipsvip.syfuse.ui.R

/**
 * 设置项风险等级（提示词 5.10：每个设置项必须有风险等级标记）。
 *
 * 三色标记的存在意义：设置项里既有「换个主题」这种无副作用项，
 * 也有「写入系统证书」这种高危项，用户需要一眼区分。
 */
enum class RiskLevel(@StringRes val labelRes: Int) {
    SAFE(R.string.risk_safe),
    ADVANCED(R.string.risk_advanced),
    DANGEROUS(R.string.risk_dangerous),
}

/**
 * 设置分组（提示词 5.10 的完整分组结构）。
 *
 * 顺序即界面顺序，且刻意与提示词 5.10 的列举顺序一致，
 * 便于与文档对照核查覆盖度。
 */
enum class SettingsGroup(@StringRes val titleRes: Int, val items: List<SettingsItem>) {
    GENERAL(
        R.string.settings_group_general,
        listOf(
            SettingsItem("theme", R.string.settings_theme, R.string.settings_theme_desc, RiskLevel.SAFE),
            SettingsItem("dynamic_color", R.string.settings_dynamic_color, R.string.settings_dynamic_color_desc, RiskLevel.SAFE),
            SettingsItem("language", R.string.settings_language, R.string.settings_language_desc, RiskLevel.SAFE),
            SettingsItem("expert_mode", R.string.settings_expert_mode, R.string.settings_expert_mode_desc, RiskLevel.ADVANCED),
            SettingsItem("autostart", R.string.settings_autostart, R.string.settings_autostart_desc, RiskLevel.ADVANCED),
        ),
    ),

    /** 网络与 DNS（提示词 5.10）。填充于 M5。 */
    NETWORK_DNS(R.string.settings_group_network_dns, emptyList()),

    /** 隧道与内核。填充于 M2。 */
    TUNNEL(R.string.settings_group_tunnel, emptyList()),

    /** HTTPS 解密。填充于 M6。 */
    HTTPS_DECRYPTION(R.string.settings_group_https_decryption, emptyList()),

    /** 脚本与重写。填充于 M6。 */
    SCRIPTS(R.string.settings_group_scripts, emptyList()),

    /** 订阅与备份。填充于 M3。 */
    SUBSCRIPTION(R.string.settings_group_subscription, emptyList()),

    /** 规则。填充于 M4。 */
    RULES(R.string.settings_group_rules, emptyList()),

    /** 统计与隐私。填充于 M7。 */
    STATS_PRIVACY(R.string.settings_group_stats_privacy, emptyList()),

    /** 特权与增强（差异增强层）。填充于 M10 / M11。 */
    PRIVILEGED(R.string.settings_group_privileged, emptyList()),

    /** 实验室。填充于 M12。 */
    LAB(R.string.settings_group_lab, emptyList()),

    /** 关于。填充于 M12。 */
    ABOUT(R.string.settings_group_about, emptyList()),
    ;

    companion object {
        /**
         * 尚未填充的分组列表。
         *
         * 为什么要显式维护：M1 只交付「通用」分组，但另外 10 个分组必须
         * 出现在导航中（提示词 5.10 要求完整分组结构），只是内容为空。
         * 这个列表让「哪些分组还是空的」变成可断言的事实，
         * 而不是需要人工翻代码才能知道。
         */
        val PENDING_GROUPS: List<SettingsGroup>
            get() = entries.filter { it.items.isEmpty() }
    }
}

/**
 * 单个设置项描述。
 *
 * @param key 持久化键，必须稳定 —— 一旦变更会导致用户设置丢失
 * @param titleRes 标题
 * @param descRes 副标题说明（提示词 5.10 三段式要求）
 * @param risk 风险等级标记
 * @param expertOnly 是否仅在专家模式下可见（提示词 5.10：高级项默认隐藏）
 */
data class SettingsItem(
    val key: String,
    @StringRes val titleRes: Int,
    @StringRes val descRes: Int,
    val risk: RiskLevel,
    val expertOnly: Boolean = false,
)
