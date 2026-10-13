// daemon-protocol —— 与守护进程 syfused 的**协议**层（请求/应答模型、客户端、传输接口）。
// 纯 JVM：不依赖 Android。
//
// 为什么必须从 daemon 里拆出来（这是实测倒逼的，不是设计洁癖）：
//
//   `DaemonClient` 本身一行 Android 代码都没有，但它原先住在 Android library 模块
//   `:daemon` 里。Android library 的**单元测试**源码集是对着 `android.jar` 编译的，
//   而 `android.jar` 里**没有** `java.net.UnixDomainSocketAddress`
//   （实测：`unzip -l android-35/android.jar | grep UnixDomainSocketAddress` 无输出；
//   `android/net/LocalSocket.class` 有）。后果是"App → UDS → 守护进程"这条往返
//   只能在真机上验证，沙箱里连**编译**都过不去。
//
//   把协议层放进纯 JVM 模块后，测试对着真正的 JDK 编译，
//   于是可以注入一个基于 AF_UNIX 的传输实现，用**本机真实编译出的 syfused 进程**
//   跑通往返（见 src/test/.../DaemonWireRoundTripTest.kt）。
//   真机专属的 `android.net.LocalSocket` 实现留在 `:daemon`，两边各自被覆盖。
//
// 与提示词 9.1 的模块清单的差异（已在 README 的偏离登记里记）：
// 多了一个 `daemon-protocol`。理由与 `core` 拆成 `core:common` + `core:rule` 相同 ——
// "必须能在只有 JDK 的环境里独立单测"这条能力，不允许被 Android 插件绑死。
plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
}

java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

dependencies {
    // 只用 domain 的错误类型契约（SyfuseResult / SyfuseError / ErrorCode），
    // 不依赖 data / ui / engine。
    api(project(":domain"))

    implementation(libs.kotlinx.serialization.json)

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.truth)
    testImplementation(libs.kotlinx.coroutines.test)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

tasks.test {
    useJUnitPlatform()
    // 往返测试会把真实守护进程的应答片段打到标准输出 —— 那些片段本身就是证据。
    testLogging {
        events("passed", "failed", "skipped")
        showStandardStreams = true
    }
}
