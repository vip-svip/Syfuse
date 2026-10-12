package io.github.vipsvip.syfuse

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dagger.hilt.android.AndroidEntryPoint
import io.github.vipsvip.syfuse.data.settings.AppSettingsDataStore
import io.github.vipsvip.syfuse.ui.nav.SyfuseApp
import javax.inject.Inject

/**
 * 主 Activity。
 *
 * R1 判定点：主界面由 [SyfuseApp] 的 Compose 树构建，
 * 本类与整个 `app` / `ui` 模块**不引用 WebView**，也不存在 `loadUrl` 调用。
 * `tests/run_module_checks.sh` 会对这一点做静态校验。
 */
@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    @Inject
    lateinit var settingsStore: AppSettingsDataStore

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent {
            SyfuseApp(settingsStore = settingsStore)
        }
    }
}
