package io.github.vipsvip.syfuse.data.security

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import android.util.Base64

/**
 * 凭据保险箱：用 Android Keystore 的 AES-256-GCM 加密节点口令 / UUID / 私钥。
 *
 * 为什么必须这样做（R5）：节点凭据一旦以明文落库，备份文件、adb backup、
 * 甚至崩溃转储都可能带出它。Keystore 里的密钥不出安全硬件/TEE，
 * 即使数据库文件被完整拷走，没有设备上的 Keystore 也解不开。
 *
 * 存储格式：`v1:<base64(iv)>:<base64(ciphertext)>`
 * 版本前缀使将来更换算法时能识别旧数据并迁移，而不是直接解密失败。
 */
interface CredentialVault {
    /** 加密并返回可落库的字符串。 */
    fun encrypt(plaintext: String, alias: String): String

    /** 解密。数据损坏或密钥丢失时返回 null，调用方必须按"凭据不可用"处理。 */
    fun decrypt(stored: String, alias: String): String?
}

class AndroidKeystoreCredentialVault : CredentialVault {

    override fun encrypt(plaintext: String, alias: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, keyFor(alias))
        val iv = cipher.iv
        val ciphertext = cipher.doFinal(plaintext.toByteArray(Charsets.UTF_8))
        return "$PREFIX:${b64(iv)}:${b64(ciphertext)}"
    }

    override fun decrypt(stored: String, alias: String): String? {
        val parts = stored.split(':')
        if (parts.size != 3 || parts[0] != PREFIX) return null
        return runCatching {
            val iv = unb64(parts[1])
            val ciphertext = unb64(parts[2])
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, keyFor(alias), GCMParameterSpec(TAG_BITS, iv))
            String(cipher.doFinal(ciphertext), Charsets.UTF_8)
        }.getOrNull()
    }

    private fun keyFor(alias: String): SecretKey {
        val keyStore = KeyStore.getInstance(PROVIDER).apply { load(null) }
        (keyStore.getEntry(alias, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, PROVIDER)
        generator.init(
            KeyGenParameterSpec.Builder(
                alias,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                // 不要求用户认证：代理需要在后台无交互地建立连接。
                // 安全性由"密钥不出 Keystore"保证，而不是由每次解锁的交互保证。
                .setUserAuthenticationRequired(false)
                .build(),
        )
        return generator.generateKey()
    }

    private fun b64(bytes: ByteArray): String = Base64.encodeToString(bytes, Base64.NO_WRAP)

    private fun unb64(text: String): ByteArray = Base64.decode(text, Base64.NO_WRAP)

    private companion object {
        const val PROVIDER = "AndroidKeyStore"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val TAG_BITS = 128
        const val PREFIX = "v1"
    }
}
