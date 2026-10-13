package io.github.vipsvip.syfuse.core.common.color

import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.pow
import kotlin.math.sin

/**
 * OKLCH 颜色空间与 sRGB 的互转，以及 WCAG 对比度计算。
 *
 * **为什么要有这个文件**：提示词 4.11 要求"全部色彩以 OKLCH token 定义"。
 * 如果把 OKLCH 只写成注释、实际填 sRGB 硬编码值，那"以 OKLCH 定义"就是假的 ——
 * 改一个 L 值要人工换算。这里做真实换算，token 文件里写的就是 OKLCH 数值。
 *
 * 放在 `core:common`（纯 JVM）而不是 `ui`：这样换算与对比度断言可以在
 * **只有 JDK** 的环境里被单测覆盖，不依赖 Android SDK 或设备。
 *
 * 参考：OKLab 由 Björn Ottosson 提出（https://bottosson.github.io/posts/oklab/）。
 * 实现用其公开的矩阵系数，不涉及任何第三方代码。
 */
object Oklch {

    /** 一个 OKLCH 颜色。L 取值 0..1，C 取值 0..~0.4，H 为角度 0..360。 */
    data class Color(val l: Double, val c: Double, val h: Double) {
        init {
            require(l in 0.0..1.0) { "L 必须在 0..1，实际 $l" }
            require(c >= 0.0) { "C 不能为负，实际 $c" }
        }
    }

    /**
     * OKLCH → 0xAARRGGBB 的 sRGB 值。
     *
     * 超出 sRGB 色域的分量会被**截断**到 [0,1]。这里刻意不做色域映射（gamut mapping）：
     * 本项目所有 token 都在 sRGB 内（已由测试断言），截断只是防御性措施。
     * 若将来引入 sRGB 外的 token，应当显式做映射而不是依赖截断 —— 那会让色相偏移。
     */
    fun toArgb(color: Color, alpha: Int = 255): Int {
        val hRad = Math.toRadians(color.h)
        val a = color.c * cos(hRad)
        val b = color.c * sin(hRad)

        // OKLab -> LMS'（立方根域）
        val lPrime = color.l + 0.3963377774 * a + 0.2158037573 * b
        val mPrime = color.l - 0.1055613458 * a - 0.0638541728 * b
        val sPrime = color.l - 0.0894841775 * a - 1.2914855480 * b

        val l = lPrime * lPrime * lPrime
        val m = mPrime * mPrime * mPrime
        val s = sPrime * sPrime * sPrime

        // LMS -> 线性 sRGB
        val rLin = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s
        val gLin = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s
        val bLin = -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s

        val r = linearToSrgb(rLin)
        val g = linearToSrgb(gLin)
        val bl = linearToSrgb(bLin)

        return (clamp8(alpha) shl 24) or (clamp8(r) shl 16) or (clamp8(g) shl 8) or clamp8(bl)
    }

    /**
     * OKLCH → `#RRGGBB` 字符串（不含 alpha）。
     * 供 tokens 文档与测试断言使用，避免各处重复拼十六进制。
     */
    fun toHex(color: Color): String = "#%06X".format(toArgb(color) and 0xFFFFFF)

    /** sRGB 传递函数反变换（线性 → 编码）。 */
    private fun linearToSrgb(x: Double): Double =
        if (x <= 0.0031308) 12.92 * x else 1.055 * x.pow(1.0 / 2.4) - 0.055

    /** sRGB 传递函数正变换（编码 → 线性）。 */
    private fun srgbToLinear(x: Double): Double =
        if (x <= 0.04045) x / 12.92 else ((x + 0.055) / 1.055).pow(2.4)

    private fun clamp8(x: Double): Int =
        (x * 255.0 + 0.5).toInt().coerceIn(0, 255)

    private fun clamp8(x: Int): Int = x.coerceIn(0, 255)

    // -----------------------------------------------------------------------
    // WCAG 对比度
    // -----------------------------------------------------------------------

    /**
     * 计算两个 0xAARRGGBB 颜色之间的 WCAG 对比度（1.0 .. 21.0）。
     *
     * 提示词 4.11 要求"正文对比度 ≥ 4.5:1，大字 ≥ 3:1，且玻璃表面上的文字必须实测通过
     * （不能靠猜）"。本函数就是那个"实测"的工具 —— tokens 测试会用它逐对断言。
     */
    fun contrastRatio(argb1: Int, argb2: Int): Double {
        val l1 = relativeLuminance(argb1)
        val l2 = relativeLuminance(argb2)
        val lighter = maxOf(l1, l2)
        val darker = minOf(l1, l2)
        return (lighter + 0.05) / (darker + 0.05)
    }

    /** WCAG 相对亮度。 */
    fun relativeLuminance(argb: Int): Double {
        val r = srgbToLinear(((argb shr 16) and 0xFF) / 255.0)
        val g = srgbToLinear(((argb shr 8) and 0xFF) / 255.0)
        val b = srgbToLinear((argb and 0xFF) / 255.0)
        return 0.2126 * r + 0.7152 * g + 0.0722 * b
    }

    /**
     * 把前景色调整为在给定背景上至少满足 [minRatio] 对比度的版本。
     *
     * 做法：保持色相与彩度，沿 L 轴朝"远离背景亮度"的方向步进。
     * 用途：玻璃表面的文字对比度必须实测通过 —— 与其手工挑色，不如让它自动满足，
     * 这样将来改玻璃透明度时文字不会突然变得不可读。
     *
     * 返回 [foreground] 本身（若已满足），否则返回调整后的颜色。
     */
    fun ensureContrast(foreground: Color, background: Color, minRatio: Double = 4.5): Color {
        val bgArgb = toArgb(background)
        if (contrastRatio(toArgb(foreground), bgArgb) >= minRatio) return foreground

        val bgLum = relativeLuminance(bgArgb)
        // 背景亮 → 文字往暗走；背景暗 → 文字往亮走
        val step = if (bgLum > 0.5) -0.02 else 0.02
        var l = foreground.l
        var best = foreground
        var bestRatio = contrastRatio(toArgb(best), bgArgb)
        repeat(50) {
            l = (l + step).coerceIn(0.0, 1.0)
            val candidate = foreground.copy(l = l)
            val ratio = contrastRatio(toArgb(candidate), bgArgb)
            if (ratio > bestRatio) {
                best = candidate
                bestRatio = ratio
            }
            if (ratio >= minRatio) return candidate
        }
        return best
    }

    /**
     * 两种颜色是否"肉眼可区分"——用于色盲友好断言。
     *
     * 判据是 OKLab 空间的欧氏距离。阈值 0.05 是经验值：低于它，
     * 正常视力也可能分不清，遑论色觉障碍用户。
     */
    fun perceptualDistance(c1: Color, c2: Color): Double {
        val (a1, b1) = toLab(c1)
        val (a2, b2) = toLab(c2)
        val dl = c1.l - c2.l
        val da = a1 - a2
        val db = b1 - b2
        return kotlin.math.sqrt(dl * dl + da * da + db * db)
    }

    private fun toLab(c: Color): Pair<Double, Double> {
        val hRad = Math.toRadians(c.h)
        return c.c * cos(hRad) to c.c * sin(hRad)
    }

    /**
     * 判断颜色是否落在 sRGB 色域内（未发生截断）。
     * token 测试用它保证"我们的 OKLCH 值不需要色域映射"。
     */
    fun inSrgbGamut(color: Color, tolerance: Double = 1e-6): Boolean {
        val hRad = Math.toRadians(color.h)
        val a = color.c * cos(hRad)
        val b = color.c * sin(hRad)
        val lPrime = color.l + 0.3963377774 * a + 0.2158037573 * b
        val mPrime = color.l - 0.1055613458 * a - 0.0638541728 * b
        val sPrime = color.l - 0.0894841775 * a - 1.2914855480 * b
        val l = lPrime * lPrime * lPrime
        val m = mPrime * mPrime * mPrime
        val s = sPrime * sPrime * sPrime
        val rgb = doubleArrayOf(
            4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
            -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
            -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s,
        )
        return rgb.all { it >= -tolerance && it <= 1.0 + tolerance }
    }
}
