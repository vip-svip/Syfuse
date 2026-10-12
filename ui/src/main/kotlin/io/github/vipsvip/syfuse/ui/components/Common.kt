package io.github.vipsvip.syfuse.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import io.github.vipsvip.syfuse.domain.settings.RiskLevel
import io.github.vipsvip.syfuse.domain.settings.SettingSpec
import io.github.vipsvip.syfuse.domain.settings.SettingType
import io.github.vipsvip.syfuse.ui.theme.StatusColors

/**
 * 通用组件。
 *
 * 7.3 要求每个页面实现四态：加载中（骨架屏）、空状态（含引导动作）、错误（含重试与原因）、无权限。
 * 这里把四态做成一组组件，而不是让每个页面各写一遍——否则迟早有页面漏掉某一态。
 */

/** 骨架屏。用静态色块而非动画噪点（7.2 禁止动态噪点背景）。 */
@Composable
fun LoadingState(
    modifier: Modifier = Modifier,
    message: String = "加载中",
) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        CircularProgressIndicator(modifier = Modifier.size(36.dp))
        Spacer(Modifier.height(12.dp))
        Text(message, style = MaterialTheme.typography.bodyMedium)
    }
}

/** 空状态。必须包含引导动作，否则用户只知道"没有东西"，不知道下一步做什么。 */
@Composable
fun EmptyState(
    title: String,
    description: String,
    modifier: Modifier = Modifier,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        Text(
            description,
            style = MaterialTheme.typography.bodyMedium,
            textAlign = TextAlign.Center,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (actionLabel != null && onAction != null) {
            Spacer(Modifier.height(16.dp))
            Button(onClick = onAction) { Text(actionLabel) }
        }
    }
}

/** 错误状态。必须显示原因，并提供重试。 */
@Composable
fun ErrorState(
    userMessage: String,
    technical: String?,
    hint: String?,
    modifier: Modifier = Modifier,
    onRetry: (() -> Unit)? = null,
) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("出错了", style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.error)
        Spacer(Modifier.height(8.dp))
        Text(userMessage, style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.Center)
        if (hint != null) {
            Spacer(Modifier.height(8.dp))
            Text(
                hint,
                style = MaterialTheme.typography.bodySmall,
                textAlign = TextAlign.Center,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (technical != null) {
            Spacer(Modifier.height(12.dp))
            Surface(
                color = MaterialTheme.colorScheme.surfaceVariant,
                shape = RoundedCornerShape(8.dp),
            ) {
                Text(
                    technical,
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(12.dp),
                )
            }
        }
        if (onRetry != null) {
            Spacer(Modifier.height(16.dp))
            Button(onClick = onRetry) { Text("重试") }
        }
    }
}

/** 无权限状态。说明为什么需要该权限，以及去哪里授予。 */
@Composable
fun PermissionState(
    permissionName: String,
    reason: String,
    modifier: Modifier = Modifier,
    actionLabel: String = "去授权",
    onAction: (() -> Unit)? = null,
) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("需要$permissionName", style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        Text(
            reason,
            style = MaterialTheme.typography.bodyMedium,
            textAlign = TextAlign.Center,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (onAction != null) {
            Spacer(Modifier.height(16.dp))
            OutlinedButton(onClick = onAction) { Text(actionLabel) }
        }
    }
}

/**
 * 风险等级标记。
 * 7.4 要求不只靠颜色传递信息：这里颜色 + 形状（圆点/方块/菱形）+ 文字三重编码。
 */
@Composable
fun RiskBadge(risk: RiskLevel, modifier: Modifier = Modifier) {
    val color = StatusColors.forRisk(risk)
    val (label, shape) = when (risk) {
        RiskLevel.SAFE -> "安全" to RoundedCornerShape(2.dp)
        RiskLevel.ADVANCED -> "进阶" to RoundedCornerShape(4.dp)
        RiskLevel.DANGEROUS -> "危险" to CircleShape
    }
    Row(
        modifier = modifier.semantics { contentDescription = "风险等级：$label" },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).clip(shape).background(color))
        Spacer(Modifier.width(6.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = color)
    }
}

/**
 * 设置项行。
 * 7.3 要求统一三段式：标题 + 副标题说明 + 控件。
 * 触控目标高度不低于 48dp（7.4）。
 */
@Composable
fun SettingRow(
    spec: SettingSpec,
    value: String?,
    onToggle: (Boolean) -> Unit,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp)
            .semantics { contentDescription = "${spec.title}。${spec.subtitle}" },
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        spec.title,
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = FontWeight.Medium,
                    )
                    if (spec.risk != RiskLevel.SAFE) {
                        Spacer(Modifier.width(8.dp))
                        RiskBadge(spec.risk)
                    }
                }
                Spacer(Modifier.height(2.dp))
                Text(
                    spec.subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (spec.type != SettingType.TOGGLE && spec.type != SettingType.ACTION) {
                    Spacer(Modifier.height(4.dp))
                    Text(
                        value ?: spec.defaultValue,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
            }

            when (spec.type) {
                SettingType.TOGGLE -> Switch(
                    checked = value == "true",
                    onCheckedChange = onToggle,
                    modifier = Modifier.semantics {
                        contentDescription = "${spec.title}开关，当前${if (value == "true") "开启" else "关闭"}"
                    },
                )
                SettingType.ACTION -> OutlinedButton(onClick = onClick) { Text("进入") }
                else -> OutlinedButton(onClick = onClick) { Text("修改") }
            }
        }
    }
}

/** 分组标题。 */
@Composable
fun SectionHeader(title: String, modifier: Modifier = Modifier) {
    Text(
        title,
        style = MaterialTheme.typography.titleSmall,
        color = MaterialTheme.colorScheme.primary,
        modifier = modifier.padding(start = 16.dp, top = 20.dp, bottom = 8.dp),
    )
}
