// Syfuse —— 多模块工程入口。
// 仓库策略：仅使用 google() 与 mavenCentral()，不引入任何需要额外凭据的私有仓库，
// 保证 CI 在干净环境可复现构建（对应验收项 16.8）。
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    // 禁止子模块自带仓库，避免依赖来源不可审计
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
        // 以下两个仓库仅在必要时启用，并用 content 过滤把可用坐标钉死在具体 group 上，
        // 避免任何传递依赖从非中心仓库被意外解析进来。
        // Xposed API：LSPosed 增强层编译期依赖（Apache-2.0）。
        maven("https://api.xposed.info/") {
            content { includeGroup("de.robv.android.xposed") }
        }
        // libsu：特权层 root shell（Apache-2.0，作者 topjohnwu）。
        maven("https://jitpack.io") {
            content { includeGroup("com.github.topjohnwu.libsu") }
        }
    }
}

rootProject.name = "Syfuse"

// 模块划分对齐提示词 9.1：app / ui / domain / data / core / engine / daemon / privileged / hook
//
// 与提示词的一处差异（已在 README 登记）：core 拆成 core:common 与 core:rule 两个子工程。
// 理由：规则引擎是纯 JVM 模块，必须能在**只有 JDK**的最小环境里独立单测
// （CI 的最小容器、以及任何人 clone 后不装 Android SDK 也能验证规则语义）。
// 把它和 Android 相关的 common 放同一个工程会让这个能力丧失。

// ---- 应用层 ----
include(":app")
include(":ui")

// ---- 领域与数据层 ----
include(":domain")
include(":data")

// ---- 内核与基础能力 ----
include(":core:common")
include(":core:rule") // 纯 JVM：过滤规则引擎，可在无 Android SDK 环境下独立单测
include(":engine")

// ---- 守护进程通信（UDS + 协议客户端） ----
include(":daemon")

// ---- 特权与 Hook ----
include(":privileged")
include(":hook") // Hook 框架层：Zygisk / LSPosed / APatch 检测与装载
