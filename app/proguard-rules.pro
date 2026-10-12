# R8 规则。
# 目的：保留内核 JNI 桥接与 Xposed 入口的符号，避免被裁剪后运行期才失败。

# 内核 JNI 方法由原生侧按名称查找，必须保留
-keepclasseswithmembernames class * {
    native <methods>;
}

# Xposed 入口由 LSPosed 框架按类名反射加载
-keep class io.github.vipsvip.syfuse.lsposed.SyfuseHookEntry { *; }
-keep class de.robv.android.xposed.** { *; }
-dontwarn de.robv.android.xposed.**

# libsu 依赖反射访问 su 相关接口
-keep class com.topjohnwu.superuser.** { *; }
-dontwarn com.topjohnwu.superuser.**

# kotlinx.serialization 生成的序列化器
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**
-keepclassmembers class kotlinx.serialization.json.** {
    *** Companion;
}
-keepclasseswithmembers class kotlinx.serialization.json.** {
    kotlinx.serialization.KSerializer serializer(...);
}
-keep,includedescriptorclasses class io.github.vipsvip.syfuse.**$$serializer { *; }
-keepclassmembers class io.github.vipsvip.syfuse.** {
    *** Companion;
}
-keepclasseswithmembers class io.github.vipsvip.syfuse.** {
    kotlinx.serialization.KSerializer serializer(...);
}

# Room 生成的实现类
-keep class * extends androidx.room.RoomDatabase { <init>(); }
-dontwarn androidx.room.paging.**

# 保留行号，便于崩溃定位（注意：这不构成遥测，日志只留在设备本地）
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile
