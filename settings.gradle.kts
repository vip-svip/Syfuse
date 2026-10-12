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

// ---- 应用层 ----
include(":app")
include(":ui")

// ---- 领域与数据层 ----
include(":domain")
include(":data")

// ---- 内核与基础能力 ----
include(":core:common")
include(":core:rule") // 纯 JVM 模块：过滤规则引擎，可在无 Android SDK 环境下独立单测
include(":engine")

// ---- 特权层 ----
include(":privileged")
include(":lsposed-bridge")
