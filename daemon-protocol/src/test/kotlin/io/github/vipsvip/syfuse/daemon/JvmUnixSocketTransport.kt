package io.github.vipsvip.syfuse.daemon

import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.ByteBuffer
import java.nio.channels.SelectionKey
import java.nio.channels.Selector
import java.nio.channels.SocketChannel
import java.nio.charset.StandardCharsets

/**
 * **仅测试用**的传输实现：基于 JDK 的 AF_UNIX 支持
 * （`java.net.UnixDomainSocketAddress`，JDK 16+；Windows 10 1803+ 亦可）。
 *
 * 为什么需要它：真机实现 `LocalSocketTransport` 依赖 `android.net.LocalSocket`
 * —— 那是 Android 框架类，在纯 JVM 单元测试里会抛 "not mocked"。
 * 没有这个测试实现，"App → UDS → 守护进程"这条往返就只能等真机才能验证。
 *
 * **这个类刻意放在 test 源集**，不进主产物：
 * 主产物必须用真机可用的 API，把 JDK-only 的实现打进 APK 会在
 * 低版本 Android 上直接 `NoClassDefFoundError`。
 *
 * 用 Selector 而不是阻塞读，是为了**真正实现超时**：SocketChannel
 * 没有 `soTimeout`（`setSoTimeout` 只作用于传统 Socket 的流式读），
 * 只靠阻塞读会让"守护进程没应答"变成测试永久挂起 ——
 * 挂起的测试比失败的测试更难排查。
 */
class JvmUnixSocketTransport : DaemonTransport {

    override fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String? {
        SocketChannel.open(StandardProtocolFamily.UNIX).use { channel ->
            channel.configureBlocking(false)
            val deadline = System.nanoTime() + timeoutMs.toLong() * 1_000_000L

            Selector.open().use { selector ->
                channel.register(selector, SelectionKey.OP_CONNECT)
                channel.connect(UnixDomainSocketAddress.of(socketPath))
                awaitReady(selector, SelectionKey.OP_CONNECT, deadline, "连接")
                if (!channel.finishConnect()) {
                    throw IllegalStateException("finishConnect 返回 false")
                }

                // **每次换阶段都必须重新 register 把兴趣集换掉。**
                // 这个坑实测踩过：只 register 一次 OP_CONNECT 之后，
                // select() 永远不会再报告"可写"，于是第一次 write 就一路
                // 等到 deadline，报出 "写入 超时" —— 一个看起来像
                // "守护进程没应答"、实际是"我根本没在等写"的假象。
                channel.register(selector, SelectionKey.OP_WRITE)
                val payload = ByteBuffer.wrap((requestLine + "\n").toByteArray(StandardCharsets.UTF_8))
                while (payload.hasRemaining()) {
                    awaitReady(selector, SelectionKey.OP_WRITE, deadline, "写入")
                    channel.write(payload)
                }

                channel.register(selector, SelectionKey.OP_READ)
                val text = StringBuilder()
                val buffer = ByteBuffer.allocate(64 * 1024)
                while (!text.contains('\n')) {
                    awaitReady(selector, SelectionKey.OP_READ, deadline, "读取")
                    buffer.clear()
                    val read = channel.read(buffer)
                    if (read < 0) break
                    buffer.flip()
                    text.append(StandardCharsets.UTF_8.decode(buffer))
                }
                val line = text.toString().substringBefore('\n')
                return line.ifEmpty { null }
            }
        }
    }

    private fun awaitReady(selector: Selector, op: Int, deadlineNanos: Long, what: String) {
        while (true) {
            val remainingMs = (deadlineNanos - System.nanoTime()) / 1_000_000L
            if (remainingMs <= 0) throw java.net.SocketTimeoutException("$what 超时")
            if (selector.select(remainingMs) > 0) {
                val ready = selector.selectedKeys()
                if (ready.any { it.isValid && it.readyOps() and op != 0 }) {
                    ready.clear()
                    return
                }
                ready.clear()
            }
        }
    }
}
