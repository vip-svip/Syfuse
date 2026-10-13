package io.github.vipsvip.syfuse.core.common.color

/**
 * 设计系统的语义色 token。
 *
 * **这里是全项目颜色的唯一来源。** UI 层不允许出现硬编码色值 ——
 * 改主题只改这一个文件。
 *
 * ## 取值依据与两处实测调整
 *
 * 基础值来自提示词 4.11。实测发现两个问题，均已在下面逐项标注：
 *
 * **问题 1：文档的 OKLCH 与它括号里的 sRGB 近似互相矛盾。**
 * 把文档 sRGB 值反算成 OKLCH，与标注值对不上（强调色色相偏差达 11.5° / 21.7°）。
 * 按 4.11 原文「全部色彩以 OKLCH token 定义」，**以 OKLCH 为准**。
 *
 * **问题 2：3 个 token 超出 sRGB 色域。**
 * 主色 dark 蓝通道线性值 1.126 > 1，两个强调色红通道为负。
 * 直接截断会改变色相（蓝色被压到 255 后色相漂移），因此
 * **降低彩度到色域内、保留 L 与 H**。调整量见各字段注释。
 *
 * 两处调整都由 [OklchTest] 断言守着，不会悄悄失效。
 */
object DesignTokens {

    // -----------------------------------------------------------------------
    // 背景与表面
    // -----------------------------------------------------------------------

    /** 深色背景底（含极光渐层）。文档 oklch(0.18 0.04 265)，在色域内，未调整。 */
    val BackgroundDark = Oklch.Color(0.18, 0.04, 265.0)

    /** 浅色背景底。文档 oklch(0.97 0.01 265)，在色域内，未调整。 */
    val BackgroundLight = Oklch.Color(0.97, 0.01, 265.0)

    /** 深色正文色。对比度实测 15.31:1（要求 ≥ 4.5）。 */
    val OnSurfaceDark = Oklch.Color(0.93, 0.01, 265.0)

    /** 浅色正文色。对比度实测 15.82:1（要求 ≥ 4.5）。 */
    val OnSurfaceLight = Oklch.Color(0.22, 0.02, 265.0)

    /** 次要文字（深色）。 */
    val OnSurfaceVariantDark = Oklch.Color(0.70, 0.015, 265.0)

    /** 次要文字（浅色）。 */
    val OnSurfaceVariantLight = Oklch.Color(0.45, 0.02, 265.0)

    // -----------------------------------------------------------------------
    // 品牌色
    // -----------------------------------------------------------------------

    /**
     * 主色（深色）。文档 oklch(0.65 0.22 285)。
     * **彩度 0.22 → 0.191**：原值超出 sRGB 色域（蓝通道线性值 1.126），
     * 截断会让紫色偏向品红。降到色域内保留色相。
     */
    val PrimaryDark = Oklch.Color(0.65, 0.191, 285.0)

    /** 主色（浅色）。文档 oklch(0.55 0.22 285)，在色域内，未调整。 */
    val PrimaryLight = Oklch.Color(0.55, 0.22, 285.0)

    /**
     * 强调色（深色）。文档 oklch(0.82 0.14 200)。
     * **彩度 0.14 → 0.136**：原值红通道线性值为负，超色域。
     */
    val AccentDark = Oklch.Color(0.82, 0.136, 200.0)

    /**
     * 强调色（浅色）。文档 oklch(0.62 0.13 200)。
     * **彩度 0.13 → 0.103**：原值红通道线性值 -0.051，超色域较多。
     */
    val AccentLight = Oklch.Color(0.62, 0.103, 200.0)

    // -----------------------------------------------------------------------
    // 状态色（语义固定：连接中=强调色、已连接=成功、错误=危险、未连接=中性）
    // -----------------------------------------------------------------------

    /** 成功 / 已连接。文档 sRGB #34D399 的 OKLCH 近似。 */
    val Success = Oklch.Color(0.76, 0.15, 160.0)

    /** 警告。文档 sRGB #FBBF24 的 OKLCH 近似。 */
    val Warning = Oklch.Color(0.82, 0.16, 84.0)

    /** 危险 / 错误。文档 sRGB #FB7185 的 OKLCH 近似。 */
    val Danger = Oklch.Color(0.70, 0.18, 16.0)

    /** 中性 / 未连接。 */
    val Neutral = Oklch.Color(0.60, 0.02, 265.0)

    /** 连接中。语义上等于强调色，单列是为了让调用点自解释。 */
    val Connecting get() = AccentDark

    // -----------------------------------------------------------------------
    // 玻璃
    // -----------------------------------------------------------------------

    /**
     * 玻璃着色不透明度（深色）。文档给 `rgba(255,255,255,0.08)`。
     *
     * 单列成常量而不是散在组件里：4.11 要求玻璃是"受控"的，
     * 不透明度是受控参数之一，必须能在一个地方调。
     */
    const val GLASS_TINT_ALPHA_DARK = 0.08f
    const val GLASS_TINT_ALPHA_LIGHT = 0.60f

    /** 1px 高光描边。文档给 `rgba(255,255,255,0.12~0.20)`，取中值。 */
    const val GLASS_STROKE_ALPHA = 0.16f

    /**
     * 供测试遍历的全部颜色 token。
     *
     * 加新 token 时**必须**登记到这里 —— 否则色域断言会漏掉它。
     * 这是"新增项默认不检查"的防线：漏登记会让断言静默失效。
     */
    fun allColorTokens(): List<Pair<String, Oklch.Color>> = listOf(
        "BackgroundDark" to BackgroundDark,
        "BackgroundLight" to BackgroundLight,
        "OnSurfaceDark" to OnSurfaceDark,
        "OnSurfaceLight" to OnSurfaceLight,
        "OnSurfaceVariantDark" to OnSurfaceVariantDark,
        "OnSurfaceVariantLight" to OnSurfaceVariantLight,
        "PrimaryDark" to PrimaryDark,
        "PrimaryLight" to PrimaryLight,
        "AccentDark" to AccentDark,
        "AccentLight" to AccentLight,
        "Success" to Success,
        "Warning" to Warning,
        "Danger" to Danger,
        "Neutral" to Neutral,
    )
}
