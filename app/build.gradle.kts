// app —— 应用壳：Application、主 Activity、VpnService、模块入口与 ABI splits。
plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "io.github.vipsvip.syfuse"
    compileSdk = 35

    defaultConfig {
        // 包名由 tools/set_owner.sh 依据解析出的 GitHub 账号规范化生成（见 README 的"账号 → 包名"映射）
        applicationId = "io.github.vipsvip.syfuse"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
        vectorDrawables { useSupportLibrary = true }
    }

    buildFeatures {
        compose = true
    }

    // 分 ABI 产物：核心内核以原生库随包交付，分 ABI 可把单包体积压到预算内（对应第 11 部分）
    splits {
        abi {
            isEnable = true
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = false
        }
    }

    // 签名配置必须在 buildTypes 之前声明：Kotlin DSL 顺序执行，
    // 若在 buildTypes 里引用尚未创建的 signingConfig 会得到 null。
    // 密钥只从环境变量 / CI Secret 读取，仓库内不落任何口令（R5）。
    signingConfigs {
        create("releaseFromEnv") {
            val storePath = System.getenv("SYFUSE_KEYSTORE_PATH")
            if (!storePath.isNullOrBlank()) {
                storeFile = file(storePath)
                storePassword = System.getenv("SYFUSE_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("SYFUSE_KEY_ALIAS")
                keyPassword = System.getenv("SYFUSE_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // 无密钥时保持未签名，而不是用 debug 密钥冒充发布产物
            if (!System.getenv("SYFUSE_KEYSTORE_PATH").isNullOrBlank()) {
                signingConfig = signingConfigs.getByName("releaseFromEnv")
            }
        }
        debug {
            applicationIdSuffix = ".debug"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

dependencies {
    implementation(project(":ui"))
    implementation(project(":domain"))
    implementation(project(":data"))
    implementation(project(":engine"))
    implementation(project(":privileged"))
    implementation(project(":lsposed-bridge"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)
}
