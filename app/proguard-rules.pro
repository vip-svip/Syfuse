# Syfuse R8 混淆规则
#
# 原则：只保留「反射 / 序列化 / JNI」必需的符号，其余全部混淆以控制体积
# （服务于 25MB 单 ABI 体积红线）。

# 保留内核 JNI 边界与 gomobile 生成的类（M2 接入 libbox 后必需）
-keep class io.github.vipsvip.syfuse.engine.** { *; }
-keep class go.** { *; }
-keep class libbox.** { *; }

# kotlinx.serialization：@Serializable 类的 companion 与 serializer 需保留
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
-keep class * extends androidx.room.RoomDatabase
-dontwarn androidx.room.paging.**

# Hilt / Dagger
-keep class dagger.hilt.** { *; }
-keep class javax.inject.** { *; }

# 保留行号，便于崩溃定位（问题排查成本远高于几 KB 体积）
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile

# 移除发布版日志，避免日志中意外泄露凭据（R5 的运行时补充）
-assumenosideeffects class android.util.Log {
    public static *** d(...);
    public static *** v(...);
}

# 去掉对旧 Java 版本的警告噪音
-dontwarn java.lang.invoke.**
-dontwarn javax.annotation.**
