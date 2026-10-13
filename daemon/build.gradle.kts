// daemon —— **Android 侧**的守护进程传输实现（`android.net.LocalSocket`）。
//
// 为什么这个模块现在只剩一个文件：
//
//   协议模型、客户端、传输接口原本都在这里，但本模块是 Android library，
//   它的**单元测试**源码集是对着 `android.jar` 编译的，而 `android.jar` 里
//   没有 `java.net.UnixDomainSocketAddress`（实测 android-35：无此类；
//   `android/net/LocalSocket.class` 有）。后果是"App → UDS → 守护进程"这条
//   往返在沙箱里**连编译都过不去**，只能等真机。
//
//   于是协议层整体搬到纯 JVM 模块 `:daemon-protocol`，在那里用**本机真实编译出的
//   syfused 进程**跑通往返；本模块只保留真机专属的那一个实现
//   （`LocalSocketTransport`，依赖 `android.net.LocalSocket`）。
//
//   本模块因此**没有单元测试**：`LocalSocketTransport` 无法在 JVM 单测里验证
//   （框架类会抛 "not mocked"），而这正是 PENDING-EXTERNAL.md 里登记的那一项。
//   刻意不写"能跑但什么都没测"的假测试来把测试任务喂绿 ——
//   Gradle 会把无测试源码的 test 任务报成 NO-SOURCE，那是诚实的状态。
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "io.github.vipsvip.syfuse.daemon"
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
    // api：:engine / :app 需要 DaemonClient / DaemonProtocol / DaemonTransport，
    // 让它们透传出去，避免每个消费方都要重复声明。
    api(project(":daemon-protocol"))
}
