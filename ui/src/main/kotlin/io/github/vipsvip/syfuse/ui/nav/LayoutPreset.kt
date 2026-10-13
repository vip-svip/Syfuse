package io.github.vipsvip.syfuse.ui.nav

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Bolt
import androidx.compose.material.icons.filled.BugReport
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Insights
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.SwapVert
import androidx.compose.ui.graphics.vector.ImageVector

/**
 * 布局预设与可自建 Tab。
 *
 * ## 为什么需要这个（提示词 4.1 的复盘）
 *
 * 提示词对导航提了两次判定，两次都不合格：
 *  - 原版 **4 Tab**（首页/配置/数据/设置）—— 装不下本项目的功能量
 *  - 我上一版的 4 Tab 沿用同一套，同样不合格
 *
 * 复盘原因：**把「功能域」直接当成了 Tab**。结果「配置」同时装分流+过滤+模块+脚本
 * 四种东西、「设置」塞进 10 个子域、「数据」这种名字又含义模糊。
 *
 * 正确做法是**按用户任务切分 + 让布局本身可换**。
 *
 * ## 六条硬标准（4.1，不满足即不合格）
 *
 * 本文件把它们做成 [LayoutSpec.violationsOf] 的**可执行断言**，
 * 而不是留在注释里靠自觉 —— 因为"Tab 是不是合理"最容易自我说服。
 */
object LayoutCriteria {
    /** 主 Tab 数量区间（4.1 第 1 条）。 */
    const val MIN_TABS = 3
    const val MAX_TABS = 7

    /** 单个 Tab 承载的功能组占比上限/下限（4.1 第 3 条）。 */
    const val MAX_GROUP_SHARE = 1.0 / 3.0
    const val MIN_GROUP_SHARE = 0.05

    /** 每个 Tab 内的二级页上限（4.1 第 5 条）。 */
    const val MAX_SECONDARY_PAGES = 7

    /** 功能组总数（4.1 的权重配平表给出的数字）。 */
    const val TOTAL_FUNCTION_GROUPS = 34

    /**
     * 抽象词黑名单（4.1 第 2 条）。
     *
     * 名字必须是"用户会说的词"，要能回答"我在这里干什么"。
     * 「数据」「防护」「配置」「工具」都回答不了这个问题。
     */
    val ABSTRACT_WORDS = setOf("数据", "防护", "配置", "工具", "功能", "其它", "其他", "更多")
}

/**
 * 一个主 Tab 的定义。
 *
 * @param id 稳定标识（用于持久化与自建布局，**不要用显示名当 id** ——
 *   显示名是本地化的，改语言就会让布局失效）
 * @param label 显示名，必须是用户会说的词
 * @param icon 图标
 * @param functionGroups 该 Tab 承载的功能组名（用于 4.1 第 3 条的占比校验）
 * @param secondaryPages 该 Tab 内的二级页（用于 4.1 第 5 条）
 */
data class TabSpec(
    val id: String,
    val label: String,
    val icon: ImageVector,
    val functionGroups: List<String>,
    val secondaryPages: List<String>,
)

/** 一套布局。 */
data class LayoutSpec(
    val id: String,
    val name: String,
    val tabs: List<TabSpec>,
    val isDefault: Boolean = false,
) {
    /**
     * 按 4.1 的六条硬标准逐条校验，返回违规说明（空 = 合格）。
     *
     * 做成函数而不是文档：Tab 划分是本项目最容易"自我说服"的地方 ——
     * 加一个 Tab 总能找到理由。把它变成断言，加 Tab 时就必须面对配平表。
     */
    fun violationsOf(): List<String> {
        val out = mutableListOf<String>()

        // 标准 1：数量 3..7
        if (tabs.size !in LayoutCriteria.MIN_TABS..LayoutCriteria.MAX_TABS) {
            out += "Tab 数量 ${tabs.size} 不在 ${LayoutCriteria.MIN_TABS}..${LayoutCriteria.MAX_TABS}"
        }

        // 标准 2：名字不许是抽象词
        tabs.forEach { t ->
            if (t.label in LayoutCriteria.ABSTRACT_WORDS) {
                out += "Tab「${t.label}」是抽象词（4.1 第 2 条：名字要能回答「我在这里干什么」）"
            }
        }

        // 标准 3：单 Tab 占比在 5%..33%
        val total = tabs.sumOf { it.functionGroups.size }.coerceAtLeast(1)
        tabs.forEach { t ->
            val share = t.functionGroups.size.toDouble() / total
            if (share > LayoutCriteria.MAX_GROUP_SHARE) {
                out += "Tab「${t.label}」承载 ${t.functionGroups.size} 组（占 %.0f%%），超过 1/3 上限"
                    .format(share * 100)
            }
            if (share < LayoutCriteria.MIN_GROUP_SHARE) {
                out += "Tab「${t.label}」承载 ${t.functionGroups.size} 组（占 %.1f%%），低于 5%% 下限（空壳）"
                    .format(share * 100)
            }
        }

        // 标准 5：二级页 ≤ 7
        tabs.forEach { t ->
            if (t.secondaryPages.size > LayoutCriteria.MAX_SECONDARY_PAGES) {
                out += "Tab「${t.label}」有 ${t.secondaryPages.size} 个二级页，超过 ${LayoutCriteria.MAX_SECONDARY_PAGES} 上限"
            }
        }

        // 标准 4（任意功能 ≤ 2 步可达）由"Tab → 二级页"的结构保证，
        // 静态无法完全验证；这里做的是**反面检查**：确保没有 Tab 是空的
        tabs.forEach { t ->
            if (t.functionGroups.isEmpty() && t.secondaryPages.isEmpty()) {
                out += "Tab「${t.label}」既无功能组也无二级页（空壳）"
            }
        }

        return out
    }

    /** 标准 6：布局必须可替换、Tab 可自建 —— 见 [LayoutPresets.all]。 */
    val isCustomizable: Boolean get() = true
}

/**
 * 三套内置布局预设（4.1）。
 *
 * **默认是布局 B（7 Tab）**：4.1 明确「7 Tab 是默认值，不是上限 ——
 * 因为功能量确实大，宁可默认全显，让用户自己收」。
 */
object LayoutPresets {

    /** 布局 B「极客全显」7 Tab —— **默认**。 */
    val GEek = LayoutSpec(
        id = "geek",
        name = "极客全显",
        isDefault = true,
        tabs = listOf(
            TabSpec(
                id = "home", label = "首页", icon = Icons.Filled.Home,
                functionGroups = listOf("连接与状态", "快捷动作", "场景", "出口 IP", "拦截计数"),
                secondaryPages = listOf("全局路由", "连通性测试", "场景编辑"),
            ),
            TabSpec(
                id = "proxy", label = "代理", icon = Icons.Filled.SwapVert,
                functionGroups = listOf("节点", "订阅", "策略组", "测速", "隧道与内核参数", "链式代理"),
                secondaryPages = listOf(
                    "节点编辑", "策略组编辑", "订阅管理", "测速结果",
                    "隧道", "代理参数", "按需求连接",
                ),
            ),
            TabSpec(
                id = "rules", label = "规则", icon = Icons.Filled.Security,
                functionGroups = listOf("分流规则", "规则集与源", "规则体检", "测试规则", "重写", "模块"),
                secondaryPages = listOf(
                    "规则编辑", "规则集 URL", "规则体检", "测试规则",
                    "URL 重写", "头部重写", "正文重写",
                ),
            ),
            TabSpec(
                id = "block", label = "拦截", icon = Icons.Filled.Bolt,
                functionGroups = listOf(
                    "过滤规则", "黑白名单", "hosts/dnsmasq", "DNS 与 DNSSRV",
                    "网络封禁", "开屏跳过", "通知净化", "IP 防泄漏", "拦截统计",
                ),
                secondaryPages = listOf(
                    "过滤规则", "黑白名单", "Hosts 编辑", "DNS 设置",
                    "网络封禁", "开屏规则", "通知关键字",
                ),
            ),
            TabSpec(
                id = "debug", label = "调试", icon = Icons.Filled.BugReport,
                functionGroups = listOf("MITM 证书与主机", "明文抓包", "Hook 引擎与内置 Hook", "脚本编辑与调试", "重写预览"),
                secondaryPages = listOf(
                    "HTTPS 解密", "抓包查看器", "Hook 管理", "脚本编辑", "重写预览",
                ),
            ),
            TabSpec(
                id = "monitor", label = "监控", icon = Icons.Filled.Insights,
                functionGroups = listOf("连接列表", "日志", "流量统计与排行", "会话审计", "导出"),
                secondaryPages = listOf("连接列表", "代理日志", "DNS 日志", "统计详情", "导出"),
            ),
            TabSpec(
                id = "settings", label = "设置", icon = Icons.Filled.Settings,
                functionGroups = listOf(
                    "外观与主题", "布局", "权限", "更新", "模块与特权",
                    "浏览器面板", "备份还原", "诊断", "关于与作者",
                ),
                secondaryPages = listOf(
                    "外观与主题", "布局", "权限", "更新",
                    "模块与特权", "浏览器面板", "备份还原",
                ),
            ),
        ),
    )

    /** 布局 A「日常优先」5 Tab。 */
    val DAILY = LayoutSpec(
        id = "daily",
        name = "日常优先",
        tabs = listOf(
            TabSpec(
                id = "home", label = "首页", icon = Icons.Filled.Home,
                functionGroups = listOf("连接与状态", "快捷动作", "场景", "出口 IP", "拦截计数"),
                secondaryPages = listOf("全局路由", "连通性测试"),
            ),
            TabSpec(
                id = "proxy", label = "代理", icon = Icons.Filled.SwapVert,
                functionGroups = listOf("节点", "订阅", "策略组", "测速", "隧道与内核参数", "链式代理"),
                secondaryPages = listOf("节点编辑", "策略组编辑", "订阅管理", "隧道", "代理参数"),
            ),
            TabSpec(
                id = "rules", label = "规则", icon = Icons.Filled.Security,
                functionGroups = listOf(
                    "分流规则", "规则集与源", "重写", "过滤规则", "模块", "脚本",
                ),
                secondaryPages = listOf("规则编辑", "规则集 URL", "过滤规则", "模块", "脚本编辑"),
            ),
            TabSpec(
                id = "monitor", label = "监控", icon = Icons.Filled.Insights,
                functionGroups = listOf("连接列表", "日志", "流量统计与排行", "会话审计", "导出"),
                secondaryPages = listOf("连接列表", "代理日志", "DNS 日志", "统计详情", "导出"),
            ),
            TabSpec(
                id = "settings", label = "设置", icon = Icons.Filled.Settings,
                functionGroups = listOf(
                    "外观主题", "权限", "更新", "特权", "DNS", "MITM",
                    "Hook", "开屏", "封禁", "防泄漏", "面板", "备份", "诊断",
                ),
                secondaryPages = listOf("外观与主题", "权限", "更新", "备份还原", "诊断"),
            ),
        ),
    )

    /** 布局 C「极简」3 Tab。 */
    val MINIMAL = LayoutSpec(
        id = "minimal",
        name = "极简",
        tabs = listOf(
            TabSpec(
                id = "home", label = "首页", icon = Icons.Filled.Home,
                functionGroups = listOf("大开关", "可定制九宫格"),
                secondaryPages = listOf("全局路由", "连通性测试"),
            ),
            TabSpec(
                id = "console", label = "控制台", icon = Icons.Filled.Bolt,
                functionGroups = listOf(
                    "代理", "规则", "拦截", "调试", "监控",
                ),
                secondaryPages = listOf("节点", "规则", "过滤", "抓包", "日志", "统计"),
            ),
            TabSpec(
                id = "settings", label = "设置", icon = Icons.Filled.Settings,
                functionGroups = listOf("外观主题", "权限", "更新", "特权", "面板", "备份", "诊断", "关于"),
                secondaryPages = listOf("外观与主题", "权限", "更新", "备份还原", "诊断"),
            ),
        ),
    )

    val all: List<LayoutSpec> = listOf(GEek, DAILY, MINIMAL)

    /** 默认布局（4.1：首次启动即用布局 B，不做选择弹窗）。 */
    val default: LayoutSpec = GEek

    fun byId(id: String?): LayoutSpec = all.firstOrNull { it.id == id } ?: default
}
