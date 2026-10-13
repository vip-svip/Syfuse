// daemon —— 与守护进程 syfused 的通信层（UDS + 换行分隔 JSON + 令牌）。
//
// 为什么单独成模块：
//  1. 提示词 9.1 要求有这个模块；
//  2. 协议实现必须与 Go 侧 src/internal/ipc **严格对齐**。放在独立模块里，
//     它就没有对 UI / 数据层的依赖，可以被单独的测试直接覆盖；
//  3. 无 root 时 App 不经过 UDS（直接以库方式加载内核），
//     这条分支由 engine 模块处理，daemon 模块只在有 root 时被用到。
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "io.github.vipsvip.syfuse.daemon"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // 契约测试是纯 JVM 的（读 Go 源码 + 跑 Kotlin 序列化器，不碰 Android 运行时），
    // 用 JUnit 5 跑。不显式声明 useJUnitPlatform() 的话 Gradle 会按 JUnit 4 找测试，
    // 结果是"任务成功、0 个用例"—— 那是最危险的一种绿。
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
    // 只用 domain 的错误类型契约，不依赖 data / ui
    api(project(":domain"))

    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.truth)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}
