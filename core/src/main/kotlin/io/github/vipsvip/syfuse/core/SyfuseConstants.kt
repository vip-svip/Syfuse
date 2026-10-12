package io.github.vipsvip.syfuse.core

/**
 * 全局常量。
 *
 * 集中管理的理由：项目标识散落在 module.prop、包名、URL Scheme、Intent action 等多处，
 * 一旦分散极易不一致（提示词 1.1 要求「不可改」的固定标识必须单一来源）。
 */
object SyfuseConstants {

    /** 项目代号，用于 UI 显示。 */
    const val APP_NAME = "Syfuse"

    /** 模块 id（KernelSU / Magisk 要求全小写）。 */
    const val MODULE_ID = "syfuse"

    /** 规范化后的 GitHub 账号，用于拼装仓库归属与更新地址。 */
    const val GITHUB_OWNER = "vip-svip"

    /** 规范化后的包名。 */
    const val APPLICATION_ID = "io.github.vipsvip.syfuse"

    /** 仓库地址，由账号拼出，禁止硬编码完整 URL。 */
    const val REPO_URL = "https://github.com/$GITHUB_OWNER/Syfuse"

    /** 自更新清单地址。 */
    const val UPDATE_JSON_URL = "https://raw.githubusercontent.com/$GITHUB_OWNER/Syfuse/main/update.json"

    /** URL Scheme 前缀。 */
    const val URL_SCHEME = "syfuse"

    /** 当前配置 schema 版本，导入旧版本时用于迁移决策。 */
    const val SCHEMA_VERSION = 1
}
