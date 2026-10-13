package io.github.vipsvip.syfuse.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.github.vipsvip.syfuse.core.common.color.DesignTokens
import io.github.vipsvip.syfuse.core.common.color.Oklch
import io.github.vipsvip.syfuse.domain.settings.RiskLevel
import io.github.vipsvip.syfuse.domain.settings.ThemeMode

/**
 * 设计系统入口。
 *
 * 颜色**全部**来自 `core:common` 的 [DesignTokens]（OKLCH 定义），
 * 这里只做「OKLCH → Compose Color」的桥接与 Material 3 映射。
 *
 * 为什么这么分：
 *  - 颜色的数学与可访问性断言在**纯 JVM 模块**里可被单测覆盖（不依赖 Android）；
 *  - UI 层不允许出现硬编码色值 —— 改主题只改 DesignTokens 一个文件。
 *
 * 视觉定位（提示词 4.11）：深色优先、明亮强调色、层叠透明、大圆角、柔和光影。
 * **不复刻**实拍的纯蓝 iOS 6 风导航栏。
 */

// ---------------------------------------------------------------------------
// OKLCH -> Compose Color
// ---------------------------------------------------------------------------

/** 把 OKLCH token 转成 Compose Color。转换逻辑在 core:common，此处只做桥接。 */
fun Oklch.Color.toComposeColor(alpha: Float = 1f): Color =
    Color(Oklch.toArgb(this, alpha = (alpha.coerceIn(0f, 1f) * 255).toInt()))

// ---------------------------------------------------------------------------
// 配色方案
// ---------------------------------------------------------------------------

private val SyfuseDarkScheme = darkColorScheme(
    primary = DesignTokens.PrimaryDark.toComposeColor(),
    onPrimary = Color.White,
    secondary = DesignTokens.AccentDark.toComposeColor(),
    onSecondary = Color(0xFF00201F),
    background = DesignTokens.BackgroundDark.toComposeColor(),
    onBackground = DesignTokens.OnSurfaceDark.toComposeColor(),
    surface = DesignTokens.BackgroundDark.toComposeColor(),
    onSurface = DesignTokens.OnSurfaceDark.toComposeColor(),
    surfaceVariant = Oklch.Color(0.26, 0.03, 265.0).toComposeColor(),
    onSurfaceVariant = DesignTokens.OnSurfaceVariantDark.toComposeColor(),
    error = DesignTokens.Danger.toComposeColor(),
    onError = Color.White,
)

private val SyfuseLightScheme = lightColorScheme(
    primary = DesignTokens.PrimaryLight.toComposeColor(),
    onPrimary = Color.White,
    secondary = DesignTokens.AccentLight.toComposeColor(),
    onSecondary = Color.White,
    background = DesignTokens.BackgroundLight.toComposeColor(),
    onBackground = DesignTokens.OnSurfaceLight.toComposeColor(),
    surface = DesignTokens.BackgroundLight.toComposeColor(),
    onSurface = DesignTokens.OnSurfaceLight.toComposeColor(),
    surfaceVariant = Oklch.Color(0.92, 0.01, 265.0).toComposeColor(),
    onSurfaceVariant = DesignTokens.OnSurfaceVariantLight.toComposeColor(),
    error = Oklch.Color(0.55, 0.20, 16.0).toComposeColor(),
    onError = Color.White,
)

/**
 * AMOLED 纯黑主题。
 *
 * 4.11 要求「AMOLED 纯黑为独立主题（此时玻璃改为纯黑半透明，避免灰雾感）」。
 * 所以这里不是把背景设成 #000000 就完事 —— **表面色也必须是纯黑**，
 * 否则卡片会在纯黑背景上显出灰雾，那正是要避免的效果。
 */
private val SyfuseAmoledScheme = SyfuseDarkScheme.copy(
    background = Color.Black,
    surface = Color.Black,
    surfaceVariant = Color(0xFF0A0A0C),
)

// ---------------------------------------------------------------------------
// 排版
// ---------------------------------------------------------------------------

/**
 * 数字特征：tabular figures。
 *
 * 速率、流量、延迟这些值在刷新时会不断变宽变窄；用比例字体会让整行左右抖动。
 * 这是**可读性**问题，不是审美偏好。
 */
private val TabularNumeric = TextStyle(fontFeatureSettings = "tnum")

val SyfuseTypography = Typography(
    displaySmall = TextStyle(fontSize = 32.sp, lineHeight = 40.sp, fontWeight = FontWeight.SemiBold),
    headlineMedium = TextStyle(fontSize = 26.sp, lineHeight = 34.sp, fontWeight = FontWeight.SemiBold),
    titleLarge = TextStyle(fontSize = 20.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold),
    titleMedium = TextStyle(fontSize = 16.sp, lineHeight = 24.sp, fontWeight = FontWeight.Medium),
    bodyLarge = TextStyle(fontSize = 16.sp, lineHeight = 24.sp, fontWeight = FontWeight.Normal),
    bodyMedium = TextStyle(fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.Normal),
    labelLarge = TextStyle(fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.Medium),
    labelSmall = TextStyle(fontSize = 11.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium),
)

/**
 * 数值文本样式（速率 / 流量 / 延迟 / 计数）。
 *
 * 单独暴露成函数而不是让各处自己拼：数字对齐在本项目反复出现，
 * 散在各处写必然有人忘掉 tabular figures。
 */
@Composable
@ReadOnlyComposable
fun numericStyle(base: TextStyle = MaterialTheme.typography.bodyMedium): TextStyle =
    base.merge(TabularNumeric).copy(fontFamily = FontFamily.Monospace, textAlign = TextAlign.End)

// ---------------------------------------------------------------------------
// 形状与间距
// ---------------------------------------------------------------------------

/** 4.11：卡片 20dp、弹层 28dp、控件 14dp。 */
val SyfuseShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(14.dp),   // 控件
    medium = RoundedCornerShape(20.dp),  // 卡片
    large = RoundedCornerShape(28.dp),   // 弹层
    extraLarge = RoundedCornerShape(28.dp),
)

/** 8dp 栅格（4.11）。 */
object Spacing {
    val xs: Dp = 4.dp
    val sm: Dp = 8.dp
    val md: Dp = 16.dp
    val lg: Dp = 24.dp
    val xl: Dp = 32.dp

    /** 卡片内边距（4.11 规定 16dp）。 */
    val cardPadding: Dp = 16.dp

    /** 分组间距（4.11 规定 24dp）。 */
    val groupGap: Dp = 24.dp
}

// ---------------------------------------------------------------------------
// 主题
// ---------------------------------------------------------------------------

/**
 * 当前视觉状态。
 *
 * 玻璃强度与是否 AMOLED 不是 Material 3 的概念，但组件需要读到
 * （决定是否模糊、模糊多少、用什么底色），所以用 CompositionLocal 传递。
 */
data class SyfuseVisualState(
    val isDark: Boolean,
    val isAmoled: Boolean,
)

val LocalSyfuseVisual = staticCompositionLocalOf {
    SyfuseVisualState(isDark = true, isAmoled = false)
}

/**
 * 应用主题。
 *
 * @param themeMode 跟随系统 / 浅色 / 深色 / AMOLED 纯黑
 * @param dynamicColor 是否用 Material You 动态取色覆盖主色。
 *   **默认 false** —— 4.11 说动态取色是「可选覆盖」而非默认，
 *   否则同一份配置在不同手机上呈现的品牌色都不一样。
 *   注意当前实现**尚未接** dynamicDarkColorScheme（那需要 Context 且要覆盖
 *   背景与表面色，否则会把 4.11 的极光背景与玻璃冲掉）。此处保留参数但不生效，
 *   属于【待核实】项，已在 README 登记。
 */
@Composable
fun SyfuseTheme(
    themeMode: ThemeMode = ThemeMode.SYSTEM,
    dynamicColor: Boolean = false,
    content: @Composable () -> Unit,
) {
    val systemDark = isSystemInDarkTheme()
    val isAmoled = themeMode == ThemeMode.AMOLED
    val isDark = when (themeMode) {
        ThemeMode.SYSTEM -> systemDark
        ThemeMode.LIGHT -> false
        ThemeMode.DARK, ThemeMode.AMOLED -> true
    }

    val scheme = when {
        isAmoled -> SyfuseAmoledScheme
        isDark -> SyfuseDarkScheme
        else -> SyfuseLightScheme
    }

    // dynamicColor 目前不改变配色（见上方 KDoc 的说明）。
    // 之所以保留参数：设置项 `dynamic_color` 已存在，UI 需要能绑定它，
    // 但**不能**假装它生效 —— 设置页会把它标为「实验性」。
    @Suppress("UNUSED_EXPRESSION")
    dynamicColor

    CompositionLocalProvider(
        LocalSyfuseVisual provides SyfuseVisualState(isDark = isDark, isAmoled = isAmoled),
    ) {
        MaterialTheme(
            colorScheme = scheme,
            typography = SyfuseTypography,
            shapes = SyfuseShapes,
            content = content,
        )
    }
}

/** 读取当前视觉状态（深色 / AMOLED），供玻璃与渐变使用。 */
val syfuseVisual: SyfuseVisualState
    @Composable @ReadOnlyComposable get() = LocalSyfuseVisual.current

// ---------------------------------------------------------------------------
// 状态色（语义固定，4.11）
// ---------------------------------------------------------------------------

/**
 * 状态色。
 *
 * 语义固定（4.11）：连接中=强调色、已连接=成功、错误=危险、未连接=中性。
 *
 * 这些颜色**不能只靠颜色区分** —— 使用方必须同时给出图标与文字
 * （见 `components/StatusBadge`）。色觉障碍用户占男性约 8%，
 * 只靠颜色编码等于对这部分用户隐藏状态。
 */
object StatusColors {
    val connected: Color get() = DesignTokens.Success.toComposeColor()
    val connecting: Color get() = DesignTokens.Connecting.toComposeColor()
    val disconnected: Color get() = DesignTokens.Neutral.toComposeColor()
    val idle: Color get() = DesignTokens.Neutral.toComposeColor()
    val error: Color get() = DesignTokens.Danger.toComposeColor()
    val warning: Color get() = DesignTokens.Warning.toComposeColor()

    /**
     * 延迟着色。阈值取 Shadowrocket 的常见口径：
     * <200ms 绿、<500ms 黄、其余红。**未知延迟不着色**（返回中性色），
     * 而不是猜一个绿色 —— 猜绿会让"还没测"看起来像"很快"。
     */
    fun forLatency(latencyMs: Int?): Color = when {
        latencyMs == null || latencyMs <= 0 -> DesignTokens.Neutral.toComposeColor()
        latencyMs < 200 -> DesignTokens.Success.toComposeColor()
        latencyMs < 500 -> DesignTokens.Warning.toComposeColor()
        else -> DesignTokens.Danger.toComposeColor()
    }

    /** 风险等级着色。三重编码中的颜色通道（另两重是图标与文字）。 */
    fun forRisk(risk: RiskLevel): Color = when (risk) {
        RiskLevel.SAFE -> DesignTokens.Success.toComposeColor()
        RiskLevel.ADVANCED -> DesignTokens.Warning.toComposeColor()
        RiskLevel.DANGEROUS -> DesignTokens.Danger.toComposeColor()
    }
}
