// 根构建脚本：只声明插件，不在此处应用。各模块按需 apply。
// 有意不引入 buildSrc / convention plugin：本工程模块数量有限，显式配置比抽象层更易审计，
// 也避免为"看起来专业"引入额外构建复杂度（对应 2.3 反过度工程条款）。
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.ksp) apply false
    alias(libs.plugins.hilt) apply false
}
