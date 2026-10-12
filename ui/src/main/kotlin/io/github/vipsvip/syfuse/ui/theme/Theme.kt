package io.github.vipsvip.syfuse.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import io.github.vipsvip.syfuse.domain.model.ThemeMode

/**
 * 主题实现（提示词 5.10 与 7.1）。
 *
 * 四种模式：浅色 / 深色 / 跟随系统 / AMOLED 纯黑。
 *
 * AMOLED 模式单独实现的原因：纯黑（#000000）在 OLED 屏上像素完全熄灭，省电显著，
 * 但 Material 的 darkColorScheme 用的是 #1C1B1F 一类的近黑，达不到「纯黑」。
 * 因此 AMOLED 模式必须显式覆盖为 #000000。
 */
@Composable
fun SyfuseTheme(
    themeMode: ThemeMode = ThemeMode.SYSTEM,
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val darkTheme = when (themeMode) {
        ThemeMode.LIGHT -> false
        ThemeMode.DARK -> true
        ThemeMode.SYSTEM -> isSystemInDarkTheme()
        ThemeMode.AMOLED -> true
    }

    val colorScheme = when {
        // AMOLED 优先于动态取色：纯黑是用户显式选择，不应被系统色覆盖
        themeMode == ThemeMode.AMOLED -> AmoledColorScheme

        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }

        darkTheme -> darkColorScheme()

        else -> lightColorScheme()
    }

    MaterialTheme(
        colorScheme = colorScheme,
        typography = SyfuseTypography,
        content = content,
    )
}

/**
 * AMOLED 纯黑色板。
 *
 * surface 与 background 强制 #000000；容器色保留 Material 的层级感，
 * 否则全黑界面会导致卡片与背景完全无法区分。
 */
private val AmoledColorScheme = darkColorScheme(
    background = Color(0xFF000000),
    surface = Color(0xFF000000),
    surfaceVariant = Color(0xFF121212),
    surfaceContainer = Color(0xFF0A0A0A),
    surfaceContainerHigh = Color(0xFF141414),
    surfaceContainerHighest = Color(0xFF1C1C1C),
    onBackground = Color(0xFFE6E1E5),
    onSurface = Color(0xFFE6E1E5),
)
