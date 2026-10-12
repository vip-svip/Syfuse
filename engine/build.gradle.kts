plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "io.github.vipsvip.syfuse.engine"
    compileSdk = 35
    defaultConfig {
        minSdk = 26
        testOptions.targetSdk = 35
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlin { jvmToolchain(17) }

    // 内核 AAR 的 ABI 过滤：只保留提示词 9.1 指定的三种 ABI。
    // 这直接服务于 25MB 单 ABI 体积红线（M0 §11.3 C3）。
    defaultConfig {
        ndk { abiFilters += listOf("arm64-v8a", "armeabi-v7a", "x86_64") }
    }
}

dependencies {
    api(project(":domain"))
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)

    // M2 将在此接入 sing-box 的 gomobile AAR（libbox）。
    // 现在不引入，是因为内核 AAR 尚未构建；引入空依赖会破坏可编译性。

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.truth)
    testRuntimeOnly(libs.junit.jupiter.engine)
}

tasks.withType<Test> { useJUnitPlatform() }
