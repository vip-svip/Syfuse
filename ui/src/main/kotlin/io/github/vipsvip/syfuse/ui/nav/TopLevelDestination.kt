package io.github.vipsvip.syfuse.ui.nav

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.Rule
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.ui.graphics.vector.ImageVector
import io.github.vipsvip.syfuse.ui.R

/**
 * 底部导航四个主 Tab（提示词 5.11）。
 *
 * 顺序必须与 Shadowrocket 的四段式对齐：首页 / 节点 / 规则 / 设置。
 * 这是 P0 主干的一部分，调整顺序会破坏「操作路径同构」。
 *
 * 图标说明：使用 Material Icons 而非搬运 Shadowrocket 的资产（提示词 15.4 禁止版权资产搬运）。
 */
enum class TopLevelDestination(
    @StringRes val labelRes: Int,
    val icon: ImageVector,
    val route: String,
) {
    HOME(R.string.tab_home, Icons.Filled.Dashboard, "home"),
    NODES(R.string.tab_nodes, Icons.Filled.Wifi, "nodes"),
    RULES(R.string.tab_rules, Icons.Filled.Rule, "rules"),
    SETTINGS(R.string.tab_settings, Icons.Filled.Settings, "settings"),
}
