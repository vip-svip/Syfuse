package io.github.vipsvip.syfuse.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import io.github.vipsvip.syfuse.domain.settings.ThemeMode

/**
 * 主题。
 *
 * 四种模式（5.10 通用分组）：浅色 / 深色 / 跟随系统 / AMOLED 纯黑。
 * AMOLED 用纯黑背景（#000000）而不是"很深的灰"：在 OLED 屏上纯黑像素不发光，
 * 这是省电的实际差异，不是审美偏好。
 *
 * 性能约束（7.2）：本主题**不包含任何模糊或半透明叠加层**。
 * SY-Fusion 的真机实测显示全屏模糊会把 P50 帧耗时从 19ms 拉到 150ms、丢帧率从 3.68% 拉到 100%，
 * 因此毛玻璃在本工程被明确禁止，而不是"谨慎使用"。
 */

// 种子色：在系统不支持动态取色时的回退。偏冷的中性蓝，避免与状态色（红/绿/黄）混淆。
private val SeedPrimaryLight = Color(0xFF1F5FA9)
private val SeedPrimaryDark = Color(0xFF9CC7FF)

private val LightScheme = lightColorScheme(
    primary = SeedPrimaryLight,
    onPrimary = Color.White,
    secondary = Color(0xFF4A6178),
    surface = Color(0xFFFBFCFE),
    surfaceVariant = Color(0xFFE2E8F0),
    background = Color(0xFFFBFCFE),
    error = Color(0xFFB3261E),
)

private val DarkScheme = darkColorScheme(
    primary = SeedPrimaryDark,
    onPrimary = Color(0xFF00325A),
    secondary = Color(0xFFB3C8DE),
    surface = Color(0xFF14181D),
    surfaceVariant = Color(0xFF2A313A),
    background = Color(0xFF101418),
    error = Color(0xFFF2B8B5),
)

private val AmoledScheme = darkColorScheme(
    primary = SeedPrimaryDark,
    onPrimary = Color(0xFF00325A),
    secondary = Color(0xFFB3C8DE),
    surface = Color(0xFF000000),
    surfaceVariant = Color(0xFF12161A),
    background = Color(0xFF000000),
    error = Color(0xFFF2B8B5),
)

private val SyfuseTypography = Typography().let { base ->
    base.copy(
        titleLarge = base.titleLarge.copy(fontWeight = FontWeight.SemiBold),
        // 设置项副标题使用较小字号但保持可读；7.4 要求字体放大到 200% 不截断，
        // 因此这里不设 maxLines，交由布局自然换行。
        bodySmall = base.bodySmall.copy(lineHeight = 16.sp),
    )
}

@Composable
fun SyfuseTheme(
    themeMode: ThemeMode = ThemeMode.SYSTEM,
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val systemDark = isSystemInDarkTheme()
    val useDark = when (themeMode) {
        ThemeMode.LIGHT -> false
        ThemeMode.DARK -> true
        ThemeMode.AMOLED -> true
        ThemeMode.SYSTEM -> systemDark
    }

    val context = LocalContext.current
    val scheme = when {
        themeMode == ThemeMode.AMOLED -> AmoledScheme
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S ->
            if (useDark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        useDark -> DarkScheme
        else -> LightScheme
    }

    MaterialTheme(
        colorScheme = scheme,
        typography = SyfuseTypography,
        content = content,
    )
}

/** 状态色。用于连接状态与延迟着色。 */
object StatusColors {
    /** 未连接。 */
    val idle = Color(0xFF7A8794)

    /** 连接中。 */
    val connecting = Color(0xFFE0A200)

    /** 已连接。 */
    val connected = Color(0xFF1E8E3E)

    /** 错误。 */
    val error = Color(0xFFB3261E)

    /** 延迟着色：<200ms 绿、<500ms 黄、其余红（5.2）。 */
    fun forLatency(latencyMs: Int?): Color = when {
        latencyMs == null -> idle
        latencyMs < 200 -> connected
        latencyMs < 500 -> connecting
        else -> error
    }

    /** 风险等级三色标记（7.3）。除颜色外还配形状与文字，不单靠颜色传递信息（7.4）。 */
    fun forRisk(risk: io.github.vipsvip.syfuse.domain.settings.RiskLevel): Color = when (risk) {
        io.github.vipsvip.syfuse.domain.settings.RiskLevel.SAFE -> connected
        io.github.vipsvip.syfuse.domain.settings.RiskLevel.ADVANCED -> connecting
        io.github.vipsvip.syfuse.domain.settings.RiskLevel.DANGEROUS -> error
    }
}
