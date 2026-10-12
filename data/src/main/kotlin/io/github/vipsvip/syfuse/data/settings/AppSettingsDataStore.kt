package io.github.vipsvip.syfuse.data.settings

import androidx.datastore.core.CorruptionException
import androidx.datastore.core.DataStore
import androidx.datastore.core.Serializer
import io.github.vipsvip.syfuse.domain.settings.AppSettings
import io.github.vipsvip.syfuse.domain.settings.SettingsCatalog
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import java.io.InputStream
import java.io.OutputStream

/**
 * 设置持久化。
 *
 * 用 DataStore 而不是 SharedPreferences：DataStore 的写入是事务性的，
 * 不会出现"设置写一半崩溃后整份偏好丢失"的情况。
 *
 * 默认值来自 [SettingsCatalog]，保证"恢复推荐值"与首次安装的取值一致——
 * 这两处若各写一份默认值，迟早会漂移。
 */
class AppSettingsDataStore(
    private val dataStore: DataStore<AppSettings>,
) {
    val settings: Flow<AppSettings> get() = dataStore.data

    suspend fun set(key: String, value: String) {
        dataStore.updateData { it.withValue(key, value) }
    }

    suspend fun setAll(values: Map<String, String>) {
        dataStore.updateData { current -> current.copy(values = current.values + values) }
    }

    suspend fun remove(key: String) {
        dataStore.updateData { it.without(key) }
    }

    /** 恢复推荐值：清空覆盖项，使所有键回到目录默认值。 */
    suspend fun resetToDefaults() {
        dataStore.updateData { AppSettings(values = SettingsCatalog.defaults()) }
    }

    /** 导出为可写入配置包的键值映射。 */
    suspend fun snapshot(): Map<String, String> = dataStore.data.first().values
}

/** DataStore 序列化器。损坏时抛 [CorruptionException]，由上层决定是清空还是保留现场。 */
object AppSettingsSerializer : Serializer<AppSettings> {

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    override val defaultValue: AppSettings = AppSettings(values = SettingsCatalog.defaults())

    override suspend fun readFrom(input: InputStream): AppSettings = try {
        val text = input.readBytes().decodeToString()
        if (text.isBlank()) defaultValue else json.decodeFromString(AppSettings.serializer(), text)
    } catch (e: SerializationException) {
        throw CorruptionException("设置数据无法解析，已保留原始文件等待用户决定是否重置", e)
    }

    override suspend fun writeTo(t: AppSettings, output: OutputStream) {
        output.write(json.encodeToString(AppSettings.serializer(), t).encodeToByteArray())
    }
}
