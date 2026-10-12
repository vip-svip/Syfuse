package io.github.vipsvip.syfuse

import android.app.Application
import dagger.hilt.android.HiltAndroidApp

/**
 * 应用入口。
 *
 * R3（零遥测）在这里是**结构性**的：本 Application 不注册任何崩溃上报、
 * 分析或广告 SDK，也不初始化任何联网组件。所有出网行为都发生在用户显式
 * 配置代理或订阅之后。
 */
@HiltAndroidApp
class SyfuseApplication : Application()
