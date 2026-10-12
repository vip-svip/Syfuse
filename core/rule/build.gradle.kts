// core:rule —— 过滤规则引擎。
// 刻意保持为纯 JVM 模块：不依赖 Android SDK，因此可以在任何装有 JDK 的环境（含 CI 的最小容器）
// 里独立编译与单测。这是本工程"可验证性"的关键设计——规则语义是差异增强层的核心，
// 它必须能在最弱的工具链上被证明正确。
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
    implementation(libs.kotlinx.serialization.json)

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.truth)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

tasks.test {
    useJUnitPlatform()
    // 语料与期望表放在仓库根 tests/ 下作为唯一事实来源，避免同一份数据出现两个副本。
    systemProperty("syfuse.corpusDir", rootProject.file("tests/corpus").absolutePath)
    // 基准测试把原始测量结果追加到仓库内文件，作为第 11 部分性能预算的证据
    systemProperty("syfuse.perfOutput", rootProject.file("tests/perf_results.txt").absolutePath)
    // 把基准开关从命令行转发进测试 JVM。
    // 注意：Gradle 的 `-D` 只作用于 Gradle 自身，不会自动进 fork 出的测试进程；
    // 必须显式转发，否则 `-Dsyfuse.runPerf=true` 会被静默忽略——那会让人误以为"跑过基准了"。
    listOf("syfuse.runPerf", "syfuse.perfRuleCount").forEach { key ->
        (project.findProperty(key)?.toString() ?: System.getProperty(key))?.let {
            systemProperty(key, it)
        }
    }
    // 百万级规则基准需要较大堆；默认 3g 足以覆盖 100 万条域名规则的编译与匹配。
    maxHeapSize = "3g"
    testLogging {
        events("passed", "failed", "skipped")
        showStandardStreams = true
    }
}
