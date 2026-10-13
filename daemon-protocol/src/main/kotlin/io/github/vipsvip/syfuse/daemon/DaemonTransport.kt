package io.github.vipsvip.syfuse.daemon

/**
 * 守护进程通信的传输层：发一行、读一行。
 *
 * **为什么把它抽成一个接口**
 *
 * `android.net.LocalSocket` 是 Android 框架类（Android 上没有
 * `java.net.UnixDomainSocketAddress`，实测 android-35 的 `android.jar`
 * 里根本没有这个类）。框架类在纯 JVM 单元测试里不可用 ——
 * 直接调用会抛 "not mocked"，于是"协议逻辑 + 真实守护进程"这条链路
 * 在沙箱里就完全无法验证。
 *
 * 把传输抽成一个单方法接口之后，测试可以注入一个基于
 * `java.net.UnixDomainSocketAddress`（JDK 16+，Windows 10 1803+ 亦支持）
 * 的实现，从而在**无设备**的情况下跑通真实往返
 * （见 `daemon-protocol/src/test/.../DaemonWireRoundTripTest.kt`）。
 *
 * 两个实现分居两个模块，各自只在能被验证的地方出现：
 *  - `:daemon`（Android library）→ `LocalSocketTransport`，真机用；
 *  - `:daemon-protocol`（纯 JVM，**仅 test 源集**）→ `JvmUnixSocketTransport`。
 *
 * **诚实边界**：这样做**不能**证明 `LocalSocket` 本身在真机上可用。
 * 那一步仍然必须真机验证（见 PENDING-EXTERNAL.md）。
 * 本接口的价值是把"协议是否正确"与"平台 API 是否可用"这两件事解耦，
 * 让前者可以在沙箱里被证明，而不是把两者捆在一起一起不可验证。
 */
interface DaemonTransport {
    /**
     * 建立连接、发一行、读一行、关闭。
     *
     * @param requestLine 已序列化的一行请求（不含换行符）。
     * @return 对端返回的一行；返回 null 表示对端未返回内容（连接被关闭）。
     * @throws Throwable 连接失败、超时等异常，由 [DaemonClient] 转成结构化错误。
     */
    fun exchange(socketPath: String, requestLine: String, timeoutMs: Int): String?
}
