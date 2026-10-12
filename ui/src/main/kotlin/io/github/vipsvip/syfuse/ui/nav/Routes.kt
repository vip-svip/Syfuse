package io.github.vipsvip.syfuse.ui.nav

/**
 * 次级页面路由（提示词 5.11：从四 Tab 进入，不占 Tab）。
 *
 * 集中定义的理由：这些路由会被通知、URL Scheme、磁贴、小组件多处引用，
 * 字符串散落会导致深链失效且难以静态检查。
 */
object Routes {
    /** 连接列表。 */
    const val CONNECTIONS = "connections"

    /** 日志。 */
    const val LOGS = "logs"

    /** 统计。 */
    const val STATS = "stats"

    /** 诊断工具集。 */
    const val TOOLS = "tools"

    /** MITM 详情。 */
    const val MITM = "mitm"

    /** 脚本编辑（带 id 参数，空串表示新建）。 */
    const val SCRIPT_EDIT = "script_edit/{scriptId}"

    /** 节点编辑。 */
    const val NODE_EDIT = "node_edit/{nodeId}"

    /** 配置编辑。 */
    const val PROFILE_EDIT = "profile_edit/{profileId}"

    /** 一键体检。 */
    const val DIAGNOSTICS = "diagnostics"

    fun scriptEdit(scriptId: String) = "script_edit/$scriptId"
    fun nodeEdit(nodeId: String) = "node_edit/$nodeId"
    fun profileEdit(profileId: String) = "profile_edit/$profileId"
}
