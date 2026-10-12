package io.github.vipsvip.syfuse

import android.app.Application
import dagger.hilt.android.HiltAndroidApp

/**
 * 应用入口。
 *
 * Hilt 要求在 Application 上标注 [HiltAndroidApp]，这是 DI 图的根。
 *
 * 注意：本类刻意不做任何「启动时自动连接」「启动时上报」之类的动作。
 * 前者违背提示词 5.9（开机自启需用户显式开启），后者违背红线条 R3（零遥测）。
 */
@HiltAndroidApp
class SyfuseApplication : Application()
