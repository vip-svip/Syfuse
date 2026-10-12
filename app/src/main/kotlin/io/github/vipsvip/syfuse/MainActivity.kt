package io.github.vipsvip.syfuse

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import dagger.hilt.android.AndroidEntryPoint
import io.github.vipsvip.syfuse.domain.model.ThemeMode
import io.github.vipsvip.syfuse.ui.nav.SyfuseApp
import io.github.vipsvip.syfuse.ui.theme.SyfuseTheme

/**
 * 唯一 Activity。
 *
 * 单 Activity + Compose 导航的取舍：相比多 Activity，单 Activity 能保证
 * 主题切换、返回栈行为、状态恢复的一致性，也便于后续接入 Compose 的
 * 预测性返回（predictive back）。
 *
 * R1 合规说明：本 Activity 的 Compose 树中不含任何 WebView 节点。
 * 由 tests/run_module_checks.sh 静态扫描 + tests/ui_assets_check 双重保证。
 */
@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        setContent {
            // M1 用本地状态；M2 起从 DataStore 读取并支持实时切换
            val themeMode by remember { mutableStateOf(ThemeMode.SYSTEM) }
            val dynamicColor by remember { mutableStateOf(true) }

            SyfuseTheme(themeMode = themeMode, dynamicColor = dynamicColor) {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background,
                ) {
                    SyfuseApp()
                }
            }
        }
    }
}
