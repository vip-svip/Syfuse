// privileged —— root / KernelSU / Magisk / APatch 能力探测与特权命令白名单封装。
// libsu 提供 root shell 与 service 绑定；所有命令必须经白名单构造，禁止拼接用户输入（对应 6.5 安全要求）。
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "io.github.vipsvip.syfuse.privileged"
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
    api(project(":domain"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.kotlinx.coroutines.android)

    // libsu：Apache-2.0。仅在特权能力可用时被调用，无 root 设备上模块功能不缺失（对应 14 的"特权"维度）。
    implementation("com.github.topjohnwu.libsu:core:6.0.0")
    implementation("com.github.topjohnwu.libsu:service:6.0.0")

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)
}
