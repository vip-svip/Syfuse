// Syfuse 根构建脚本
// 说明：插件以 apply false 声明，由各模块按需应用，避免版本分散。
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.ksp) apply false
    alias(libs.plugins.hilt) apply false
}

// 供全部子模块继承的公共坐标（包名与版本号集中在此，禁止散落硬编码）
extra["syfuseNamespacePrefix"] = "io.github.vipsvip.syfuse"
