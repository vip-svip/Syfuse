// hook —— Hook 框架层（Zygisk / LSPosed / APatch）。
// 设计要点：本模块对外只暴露 Hook 契约（作用域、开关、命中统计、失败静默回退），
// Xposed API 以 compileOnly 引入，运行期由 LSPosed 框架提供实现。
// 非 LSPosed 设备上该模块不参与任何代码路径，主功能不缺失（对应 6.6）。
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "io.github.vipsvip.syfuse.hook"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)

    // 编译期类型；运行期由 LSPosed 注入。Apache-2.0。
    compileOnly("de.robv.android.xposed:api:82")
}
