plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "io.github.vipsvip.syfuse.lsposed"
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

    // LSPosed 模块需要声明 xposedmodule 元数据。
    // M1 只建立模块骨架与资源占位，Hook 实现在 M11。
    // 编译期不引入 Xposed API 依赖，避免在非 LSPosed 设备上引入无用类。
}

dependencies {
    implementation(project(":core"))
    implementation(project(":domain"))

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.truth)
    testRuntimeOnly(libs.junit.jupiter.engine)
}

tasks.withType<Test> { useJUnitPlatform() }
