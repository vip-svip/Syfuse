// ui —— Compose 设计系统与全部界面。
// 性能约束（来自 7.2 的实测教训）：禁止大面积 blur / 毛玻璃 / 动态噪点，滚动容器全部虚拟化。
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "io.github.vipsvip.syfuse.ui"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
    }

    buildFeatures {
        compose = true
    }

    // 玻璃策略层（GlassIntensity / GlassPolicy / GlassLayerCounter / FrameMonitor /
    // GlassPerformanceController）是**纯 Kotlin 逻辑**，不碰 Android 运行时，
    // 因此可以在 JVM 上直接单测。用 JUnit 5（与 core:* / domain 保持一致）。
    testOptions {
        unitTests.all {
            it.useJUnitPlatform()
        }
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
    // ui 的 ViewModel 通过构造注入消费数据层契约（ConfigRepository / AppSettingsDataStore）。
    // 这是**接口**依赖，不是内核具体类型依赖：内核可替换性（9.2）由 domain:engine.EngineController
    // 这一层保证，UI 不感知任何内核实现类。缺此依赖会让 Hilt/KSP 在 :ui 里得到
    // error.NonExistentClass（已实测），因为注入点类型必须在编译类路径上可解析。
    implementation(project(":data"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.navigation.compose)

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons.extended)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.hilt.android)
    implementation(libs.hilt.navigation.compose)
    ksp(libs.hilt.compiler)

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.truth)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}
