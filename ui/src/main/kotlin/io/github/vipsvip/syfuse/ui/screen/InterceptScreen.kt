package io.github.vipsvip.syfuse.ui.screen

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.vipsvip.syfuse.ui.components.SectionHeader
import io.github.vipsvip.syfuse.ui.vm.ToolkitViewModel
import kotlinx.serialization.json.JsonObject

/**
 * 拦截 页（提示词 4.1 布局 B 的 Tab）。
 *
 * **已接真实能力**（M10–M13）：过滤引擎统计、config.reload 热重载、
 * Hook 框架检测、防泄漏一键修复、通知净化试跑、备份创建 —— 全部经
 * UDS 调用守护进程，数据是真实的，没有假数据。
 *
 * 仍未实现的入口（开屏跳过 / UID 分流 / DNS 管理 UI）继续显式标注
 * 「未实现」，不伪装可用（12.2 第 4 条）。
 */
@Composable
fun InterceptScreen(
    vm: ToolkitViewModel = hiltViewModel(),
) {
    val st by vm.uiState.collectAsStateWithLifecycle()
    var notifyPkg by remember { mutableStateOf("com.example.app") }
    var notifyTitle by remember { mutableStateOf("") }
    var notifyContent by remember { mutableStateOf("") }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SectionHeader("拦截")

        // ---- 过滤引擎（M10）----
        ToolCard(
            title = "过滤引擎（AGH / uBO）",
            subtitle = "域名 Trie + 正则 + 优先级链；规则文件 <data>/filter_rules.txt",
        ) {
            st.filterStats?.let { stats ->
                InfoLine("规则行数", JsonInt(stats, "lines").toString())
                InfoLine("拦截规则", JsonInt(stats, "block_rules").toString())
                InfoLine("例外规则", JsonInt(stats, "allow_rules").toString())
                InfoLine("解析错误", JsonInt(stats, "errors").toString())
            } ?: InfoLine("状态", "尚未加载")
            st.error["filter"]?.let { ErrLine(it) }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { vm.refreshFilter() }, enabled = st.busy != "filter") {
                    Text("刷新统计")
                }
                Button(onClick = { vm.reloadConfig() }, enabled = st.busy != "reload") {
                    Text(if (st.busy == "reload") "重载中…" else "热重载规则")
                }
            }
            st.error["reload"]?.let { ErrLine("重载失败: " + it) }
        }

        // ---- 通知净化（M13）----
        ToolCard(
            title = "通知净化",
            subtitle = "规则文件 <data>/notify_keywords.conf（关键字 / pkg: / re:）",
        ) {
            OutlinedTextField(
                value = notifyPkg, onValueChange = { notifyPkg = it },
                label = { Text("包名（可空）") }, modifier = Modifier.fillMaxWidth(),
                singleLine = true,
            )
            OutlinedTextField(
                value = notifyTitle, onValueChange = { notifyTitle = it },
                label = { Text("通知标题") }, modifier = Modifier.fillMaxWidth(),
                singleLine = true,
            )
            OutlinedTextField(
                value = notifyContent, onValueChange = { notifyContent = it },
                label = { Text("通知正文") }, modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = { vm.notifyTest(notifyPkg, notifyTitle, notifyContent) },
                enabled = st.busy != "notify",
            ) {
                Text(if (st.busy == "notify") "测试中…" else "试跑规则")
            }
            st.notifyTest?.let { r ->
                InfoLine("判定", if (JsonBool(r, "matched")) "命中 → 拦截" else "放行")
                JsonStr(r, "rule")?.let { InfoLine("命中规则", it) }
            }
            st.error["notify"]?.let { ErrLine(it) }
        }

        // ---- IP 防泄漏 · 一键修复（M12/M13）----
        ToolCard(
            title = "IP 防泄漏 · 一键修复",
            subtitle = "七维自检（DNS/IPv6/STUN/QUIC/回显/分流/系统）→ 修复计划 → 配置级应用",
        ) {
            st.leakFix?.let { r ->
                val report = r["report"] as? JsonObject
                InfoLine("自检结论", report?.let { JsonStr(it, "overall") } ?: "—")
                val plan = r["plan"] as? List<*>
                InfoLine("修复项", (plan?.size ?: 0).toString())
                val applied = r["applied"] as? List<*>
                InfoLine(
                    "已应用",
                    if (applied.isNullOrEmpty()) "无（或已修复）" else applied.joinToString(", "),
                )
            }
            st.error["leak"]?.let { ErrLine(it) }
            Button(onClick = { vm.leakFix() }, enabled = st.busy != "leak") {
                Text(if (st.busy == "leak") "自检+修复中…" else "自检并修复")
            }
        }

        // ---- Hook 框架检测（M11）----
        ToolCard(
            title = "Hook 框架检测",
            subtitle = "Zygisk / LSPosed / APatch / Shamiko 路径探测",
        ) {
            st.hookStatus?.let { h ->
                InfoLine("Root 形态", JsonStr(h, "root_provider") ?: "none")
                InfoLine("注入能力", if (JsonBool(h, "injection_capable")) "可用" else "无（降级方案生效）")
                val arr = h["detected"] as? List<JsonObject> ?: emptyList()
                if (arr.isEmpty()) InfoLine("检测到框架", "无")
                else arr.forEach { d -> InfoLine("框架", JsonStr(d, "name") ?: "?") }
                JsonStr(h, "degraded_mode")?.let { InfoLine("降级说明", it) }
            } ?: InfoLine("状态", "尚未检测")
            st.error["hook"]?.let { ErrLine(it) }
            OutlinedButton(onClick = { vm.refreshHook() }, enabled = st.busy != "hook") {
                Text("重新检测")
            }
        }

        // ---- 备份还原（M13）----
        ToolCard(
            title = "备份还原",
            subtitle = "打包 config/rules/scripts/modules（SHA-256 清单防半还原）",
        ) {
            st.backupPath?.let { InfoLine("备份包", it) }
            st.error["backup"]?.let { ErrLine(it) }
            Button(onClick = { vm.backupCreate() }, enabled = st.busy != "backup") {
                Text(if (st.busy == "backup") "打包中…" else "创建备份")
            }
        }

        // ---- 仍未实现（如实标注，不做死 UI）----
        SectionHeader("未实现（依赖真机）")
        Text(
            text = "以下功能依赖 Android 真机环境，入口不伪装可用：",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        GroupRow("开屏跳过（Hook / 无障碍）", "真机验证")
        GroupRow("UID 精细分流（iptables）", "真机验证")
        GroupRow("黑白名单编辑 / DNS 管理 UI", "M13 后段")
    }
}

@Composable
private fun ToolCard(title: String, subtitle: String, content: @Composable () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .semantics { contentDescription = title },
        colors = CardDefaults.cardColors(),
    ) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            content()
        }
    }
}

@Composable
private fun InfoLine(label: String, value: String) {
    Row(Modifier.fillMaxWidth()) {
        Text(
            label,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(end = 8.dp),
        )
        Text(value, style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
private fun ErrLine(message: String) {
    Text(
        "错误：$message",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.error,
    )
}

// JSON 取值小工具（容错：字段缺失/类型不符返回默认值）。
private fun JsonInt(obj: JsonObject, key: String): Int =
    (obj[key] as? kotlinx.serialization.json.JsonPrimitive)?.content?.toIntOrNull() ?: 0

private fun JsonBool(obj: JsonObject, key: String): Boolean =
    (obj[key] as? kotlinx.serialization.json.JsonPrimitive)?.content == "true"

private fun JsonStr(obj: JsonObject, key: String): String? =
    (obj[key] as? kotlinx.serialization.json.JsonPrimitive)?.content

@Composable
private fun GroupRow(name: String, milestone: String) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        colors = CardDefaults.cardColors(),
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(12.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(name, style = MaterialTheme.typography.bodyMedium)
            Text(
                "未实现 · $milestone",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
