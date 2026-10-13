package io.github.vipsvip.syfuse.core.common.color

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.DisplayName
import org.junit.jupiter.api.Test

/**
 * OKLCH 换算与可访问性的实测断言。
 *
 * ## 关于「文档近似值」的勘误（实测，不是猜测）
 *
 * 提示词 4.11 给的是 `oklch(...) ≈ #XXXXXX` 形式。实测发现**两套值互相矛盾**：
 * 把文档里的 sRGB 值反算成 OKLCH，与它标注的 OKLCH 对不上：
 *
 * | token | 文档 sRGB 反算 | 文档标注 | 差异 |
 * |---|---|---|---|
 * | 主色 dark | L=0.599 C=0.230 H=286.2 | L=0.65 C=0.22 H=285 | dL=-0.051 |
 * | 强调色 dark | L=0.797 C=0.134 H=211.5 | L=0.82 C=0.14 H=200 | **dH=+11.5°** |
 * | 主色 light | L=0.540 C=0.184 H=278.3 | L=0.55 C=0.22 H=285 | dC=-0.036 |
 * | 强调色 light | L=0.609 C=0.111 H=221.7 | L=0.62 C=0.13 H=200 | **dH=+21.7°** |
 * | 背景底 dark | L=0.177 C=0.034 H=269.6 | L=0.18 C=0.04 H=265 | 基本自洽 |
 * | 背景底 light | L=0.977 C=0.005 H=275.0 | L=0.97 C=0.01 H=265 | 基本自洽 |
 *
 * 11.5° 与 21.7° 的色相偏差不是四舍五入能解释的 —— 两套值来自不同的取色过程。
 *
 * **本项目的裁决**（依据 4.11 的原文「全部色彩以 OKLCH token 定义」）：
 * **以 OKLCH 为准，sRGB 括号值视为近似**。另外实测发现 3 个 token 超出 sRGB 色域
 * （主色 dark 蓝通道线性值 1.126 > 1；两个强调色红通道为负），
 * 直接截断会导致色相偏移，因此**降低彩度到色域内、保留 L 与 H**。
 * 具体调整见 [io.github.vipsvip.syfuse.core.common.color.DesignTokens]。
 *
 * 本测试的作用：把上面的结论**变成可执行的断言**，而不是留在注释里。
 */
class OklchTest {

    /**
     * 与文档近似值的容差。
     *
     * 设成 34 是因为实测最大偏差就是 34（强调色 dark）——
     * 这个数字本身就是结论：文档的 sRGB 近似**不能当作精确值使用**。
     * 之所以仍然断言"在容差内"，是为了防止换算实现出现方向性错误
     * （例如把 OKLab 的 a/b 轴搞反，那会得到完全不同的颜色）。
     */
    private val docTolerance = 34

    private fun assertCloseToHex(color: Oklch.Color, expectedHex: String, label: String) {
        val actual = Oklch.toArgb(color) and 0xFFFFFF
        val expected = expectedHex.removePrefix("#").toInt(16)
        val ar = (actual shr 16) and 0xFF; val ag = (actual shr 8) and 0xFF; val ab = actual and 0xFF
        val er = (expected shr 16) and 0xFF; val eg = (expected shr 8) and 0xFF; val eb = expected and 0xFF
        val dr = kotlin.math.abs(ar - er); val dg = kotlin.math.abs(ag - eg); val db = kotlin.math.abs(ab - eb)
        val maxDev = maxOf(dr, dg, db)
        println("  [OKLCH] $label: 计算=#%02X%02X%02X 文档近似=%s 偏差=(%d,%d,%d)"
            .format(ar, ag, ab, expectedHex, dr, dg, db))
        assertThat(maxDev).isAtMost(docTolerance)
    }

    // -----------------------------------------------------------------------
    // 1. 换算方向正确性：结果必须落在文档近似值附近
    // -----------------------------------------------------------------------

    @Test
    @DisplayName("深色主题色换算方向正确（落在 4.11 文档近似值容差内）")
    fun darkTokensMatchDocumentedSrgb() {
        assertCloseToHex(Oklch.Color(0.18, 0.04, 265.0), "#0B1020", "背景底 dark")
        assertCloseToHex(Oklch.Color(0.65, 0.22, 285.0), "#7C5CFF", "主色 dark")
        assertCloseToHex(Oklch.Color(0.82, 0.14, 200.0), "#22D3EE", "强调色 dark")
    }

    @Test
    @DisplayName("浅色主题色换算方向正确（落在 4.11 文档近似值容差内）")
    fun lightTokensMatchDocumentedSrgb() {
        assertCloseToHex(Oklch.Color(0.97, 0.01, 265.0), "#F6F7FB", "背景底 light")
        assertCloseToHex(Oklch.Color(0.55, 0.22, 285.0), "#5B5BD6", "主色 light")
        assertCloseToHex(Oklch.Color(0.62, 0.13, 200.0), "#0891B2", "强调色 light")
    }

    @Test
    @DisplayName("3 个文档 token 确实超出 sRGB 色域（勘误的实证）")
    fun documentedTokensAreOutOfSrgbGamut() {
        val outOfGamut = listOf(
            "主色 dark" to Oklch.Color(0.65, 0.22, 285.0),
            "强调色 dark" to Oklch.Color(0.82, 0.14, 200.0),
            "强调色 light" to Oklch.Color(0.62, 0.13, 200.0),
        )
        for ((label, c) in outOfGamut) {
            println("  [色域] $label 文档值在 sRGB 内 = ${Oklch.inSrgbGamut(c)}")
            assertThat(Oklch.inSrgbGamut(c)).isFalse()
        }
        // 两个背景色在色域内 —— 说明不是实现问题，是那几个值本身的问题
        assertThat(Oklch.inSrgbGamut(Oklch.Color(0.18, 0.04, 265.0))).isTrue()
        assertThat(Oklch.inSrgbGamut(Oklch.Color(0.97, 0.01, 265.0))).isTrue()
    }

    @Test
    @DisplayName("本项目采用的 token 全部在 sRGB 色域内（不需要色域映射）")
    fun adoptedTokensAreInGamut() {
        for ((label, c) in DesignTokens.allColorTokens()) {
            val ok = Oklch.inSrgbGamut(c)
            println("  [色域] $label = ${Oklch.toHex(c)} 在 sRGB 内 = $ok")
            assertThat(ok).isTrue()
        }
    }

    // -----------------------------------------------------------------------
    // 2. 换算本身的数学性质
    // -----------------------------------------------------------------------

    @Test
    @DisplayName("L=0 是黑、L=1 是白（C=0 时）")
    fun achromaticEndpoints() {
        assertThat(Oklch.toArgb(Oklch.Color(0.0, 0.0, 0.0)) and 0xFFFFFF).isEqualTo(0x000000)
        assertThat(Oklch.toArgb(Oklch.Color(1.0, 0.0, 0.0)) and 0xFFFFFF).isEqualTo(0xFFFFFF)
    }

    @Test
    @DisplayName("C=0 时结果与色相无关（灰色轴）")
    fun grayIsHueIndependent() {
        val a = Oklch.toArgb(Oklch.Color(0.5, 0.0, 0.0))
        assertThat(Oklch.toArgb(Oklch.Color(0.5, 0.0, 137.0))).isEqualTo(a)
        assertThat(Oklch.toArgb(Oklch.Color(0.5, 0.0, 300.0))).isEqualTo(a)
    }

    @Test
    @DisplayName("alpha 通道被正确写入且被夹紧")
    fun alphaChannel() {
        assertThat((Oklch.toArgb(Oklch.Color(0.5, 0.1, 200.0), alpha = 255) ushr 24) and 0xFF).isEqualTo(255)
        assertThat((Oklch.toArgb(Oklch.Color(0.5, 0.1, 200.0), alpha = 128) ushr 24) and 0xFF).isEqualTo(128)
        assertThat((Oklch.toArgb(Oklch.Color(0.5, 0.1, 200.0), alpha = 300) ushr 24) and 0xFF).isEqualTo(255)
        assertThat((Oklch.toArgb(Oklch.Color(0.5, 0.1, 200.0), alpha = -5) ushr 24) and 0xFF).isEqualTo(0)
    }

    @Test
    @DisplayName("越界 C 被截断而不是回绕")
    fun outOfGamutIsClamped() {
        val argb = Oklch.toArgb(Oklch.Color(0.5, 0.4, 140.0))
        for (shift in intArrayOf(16, 8, 0)) {
            assertThat((argb shr shift) and 0xFF).isAtLeast(0)
            assertThat((argb shr shift) and 0xFF).isAtMost(255)
        }
        assertThat(Oklch.inSrgbGamut(Oklch.Color(0.5, 0.4, 140.0))).isFalse()
    }

    // -----------------------------------------------------------------------
    // 3. WCAG 对比度（4.11 的硬要求：正文 ≥ 4.5:1，大字 ≥ 3:1）
    // -----------------------------------------------------------------------

    @Test
    @DisplayName("对比度公式的已知边界")
    fun contrastKnownValues() {
        assertThat(Oklch.contrastRatio(0xFFFFFF, 0x000000)).isWithin(0.01).of(21.0)
        assertThat(Oklch.contrastRatio(0xFFFFFF, 0xFFFFFF)).isWithin(0.01).of(1.0)
        assertThat(Oklch.contrastRatio(0x000000, 0x000000)).isWithin(0.01).of(1.0)
        assertThat(Oklch.contrastRatio(0xFFFFFF, 0x000000))
            .isWithin(1e-9).of(Oklch.contrastRatio(0x000000, 0xFFFFFF))
    }

    @Test
    @DisplayName("4.11 要求：深色主题正文对比度 ≥ 4.5:1")
    fun darkBodyTextContrast() {
        val ratio = Oklch.contrastRatio(
            Oklch.toArgb(DesignTokens.OnSurfaceDark),
            Oklch.toArgb(DesignTokens.BackgroundDark),
        )
        println("  [对比度] 深色 正文/背景 = %.2f:1（要求 ≥ 4.5）".format(ratio))
        assertThat(ratio).isAtLeast(4.5)
    }

    @Test
    @DisplayName("4.11 要求：浅色主题正文对比度 ≥ 4.5:1")
    fun lightBodyTextContrast() {
        val ratio = Oklch.contrastRatio(
            Oklch.toArgb(DesignTokens.OnSurfaceLight),
            Oklch.toArgb(DesignTokens.BackgroundLight),
        )
        println("  [对比度] 浅色 正文/背景 = %.2f:1（要求 ≥ 4.5）".format(ratio))
        assertThat(ratio).isAtLeast(4.5)
    }

    @Test
    @DisplayName("4.11 要求：主色上的白字对比度 ≥ 3:1（大字/按钮）")
    fun primaryButtonTextContrast() {
        val ratio = Oklch.contrastRatio(0xFFFFFF, Oklch.toArgb(DesignTokens.PrimaryDark))
        println("  [对比度] 白字/主色 = %.2f:1（要求 ≥ 3.0）".format(ratio))
        assertThat(ratio).isAtLeast(3.0)
    }

    @Test
    @DisplayName("ensureContrast 能把不达标的组合调到达标，且保留色相与彩度")
    fun ensureContrastFixesBadPairs() {
        val bg = DesignTokens.BackgroundDark
        val badFg = Oklch.Color(0.28, 0.02, 265.0)
        val before = Oklch.contrastRatio(Oklch.toArgb(badFg), Oklch.toArgb(bg))
        val fixed = Oklch.ensureContrast(badFg, bg, minRatio = 4.5)
        val after = Oklch.contrastRatio(Oklch.toArgb(fixed), Oklch.toArgb(bg))

        println("  [ensureContrast] 修正前 %.2f:1 -> 修正后 %.2f:1".format(before, after))
        assertThat(before).isLessThan(4.5)
        assertThat(after).isAtLeast(4.5)
        // 只调明度，色相与彩度必须保留 —— 否则品牌色会被改掉
        assertThat(fixed.h).isWithin(1e-9).of(badFg.h)
        assertThat(fixed.c).isWithin(1e-9).of(badFg.c)
    }

    @Test
    @DisplayName("ensureContrast 对已达标的颜色原样返回")
    fun ensureContrastNoopWhenAlreadyOk() {
        assertThat(Oklch.ensureContrast(DesignTokens.OnSurfaceDark, DesignTokens.BackgroundDark))
            .isEqualTo(DesignTokens.OnSurfaceDark)
    }

    // -----------------------------------------------------------------------
    // 4. 色盲友好：语义色之间必须可区分
    // -----------------------------------------------------------------------

    @Test
    @DisplayName("状态色两两在 OKLab 空间可区分（色盲友好的必要条件）")
    fun statusColorsAreDistinguishable() {
        val pairs = listOf(
            "成功/警告" to (DesignTokens.Success to DesignTokens.Warning),
            "成功/危险" to (DesignTokens.Success to DesignTokens.Danger),
            "成功/中性" to (DesignTokens.Success to DesignTokens.Neutral),
            "警告/危险" to (DesignTokens.Warning to DesignTokens.Danger),
            "警告/中性" to (DesignTokens.Warning to DesignTokens.Neutral),
            "危险/中性" to (DesignTokens.Danger to DesignTokens.Neutral),
        )
        for ((label, pair) in pairs) {
            val d = Oklch.perceptualDistance(pair.first, pair.second)
            println("  [色差] %s = %.4f".format(label, d))
            assertThat(d).isAtLeast(0.05)
        }
    }
}
