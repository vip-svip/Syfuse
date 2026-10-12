pluginManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
        // libsu 发布在 JitPack
        maven { url = uri("https://jitpack.io") }
    }
}

rootProject.name = "Syfuse"

include(":app")
include(":ui")
include(":domain")
include(":data")
include(":core")
include(":engine")
include(":privileged")
include(":lsposed-bridge")
