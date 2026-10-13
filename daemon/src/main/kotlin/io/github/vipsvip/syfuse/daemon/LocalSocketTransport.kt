package io.github.vipsvip.syfuse.daemon

import android.net.LocalSocket
import android.net.LocalSocketAddress
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.nio.charset.StandardCharsets

/**
 * 真机实现：基于 [LocalSocket]。
 *
 * **为什么本文件留在 `:daemon`（Android library）而不是 `:daemon-protocol`**
 *
 * `android.net.LocalSocket` 是 Android 框架类，纯 JVM 模块里根本没有它，
 * 放进去连编译都过不去。而它又**必须**是真机用的实现 ——
 * Android 上不存在 `java.net.UnixDomainSocketAddress`
 * （实测 android-35 的 `android.jar` 无此类）。
 *
 * 于是这里就是那条"只能在真机上被验证"的边界：
 * 协议逻辑（[DaemonProtocol] / [DaemonClient]）在 `:daemon-protocol` 里
 * 用真实进程往返证明；[LocalSocket] 这一段在真机上验证，
 * 记为 PENDING-EXTERNAL.md 里的待外部验证项，**不得**被含糊地算作已验证。
 *
 * 用 LocalSocket 而不是 java.nio 的 UnixDomainSocketAddress，是因为
 * 后者不在 Android 的 API 面里（见上）；而 LocalSocket 自 API 1 起就存在，
 * 覆盖本项目的 minSdk 26。
 */
class LocalSocketTransport : DaemonTransport {
    override fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String? {
        LocalSocket().use { socket ->
            socket.connect(
                LocalSocketAddress(socketPath, LocalSocketAddress.Namespace.FILESYSTEM),
            )
            socket.soTimeout = timeoutMs

            // 读必须发生在 writer 的 use 块**之内**：writer 关闭会连带关闭
            // 底层 socket，先关再读会永远拿不到应答。这个嵌套顺序是有意的。
            BufferedWriter(OutputStreamWriter(socket.outputStream, StandardCharsets.UTF_8)).use { w ->
                w.write(requestLine)
                w.write("\n")
                w.flush()

                return BufferedReader(
                    InputStreamReader(socket.inputStream, StandardCharsets.UTF_8),
                ).readLine()
            }
        }
    }
}
