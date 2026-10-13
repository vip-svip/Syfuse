// engine —— 内核抽象与 IPC 契约。
// 关键设计：UI 层只依赖本模块暴露的接口，不依赖任何具体内核类型（对应 9.2 的内核可替换要求）。
// 当前提供 JVM 内嵌的参考实现（ReferenceEngine），Go 内核 AAR 接入后可直接替换，无需改 UI。
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "io.github.vipsvip.syfuse.engine"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
    }

    // 与 daemon 模块同样的理由：测试用 JUnit 5，不显式声明 useJUnitPlatform()
    // 的话 Gradle 会按 JUnit 4 找测试，结果是"任务成功、0 个用例" ——
    // 那是最危险的一种绿。
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

    // 走 UDS 的数据面（UdsDataPlane）需要守护进程客户端。
    // daemon 模块只依赖 domain 的错误类型契约，不反向依赖本模块，无循环。
    implementation(project(":daemon"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.truth)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}
