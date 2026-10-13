package io.github.vipsvip.syfuse.domain.node

import io.github.vipsvip.syfuse.domain.model.ProxyNode
import io.github.vipsvip.syfuse.domain.model.ProxyProtocol
import io.github.vipsvip.syfuse.domain.model.TlsOptions
import io.github.vipsvip.syfuse.domain.model.TransportOptions
import io.github.vipsvip.syfuse.domain.model.TransportType

/**
 * [ShareNode]（"从一条链接解析出来的事实"）→ [ProxyNode]（"可持久化的实体"）的映射。
 *
 * 为什么要有这一层：两个模型职责不同。
 *  - [ShareNode] 只承载解析结果，没有 `id` / `sortIndex` / 凭据引用等持久化概念；
 *  - [ProxyNode] 带 `id`、`credentialRef`（**密文引用，不是明文**）等落库字段。
 *
 * 因此从订阅导入节点时必须显式过一遍这里，而不是让两个模型互相耦合。
 * 这一层也是 **R5（凭据不落明文）** 的执行点：明文口令/UUID **不进** [ProxyNode]，
 * 只写 [ProxyNode.credentialRef]，由上层负责把明文交给凭据保险箱。
 */

/**
 * 把解析结果映射为可持久化节点。
 *
 * @param id 目标节点的稳定主键（由调用方给出，通常取自订阅行号或 [ShareNode.key] 的哈希）。
 * @param sortIndex 订阅内的顺序，供 UI 保持原始排列。
 * @param credentialRef 凭据保险箱引用。**默认 null** —— 调用方必须在真正写入明文凭据后
 *   回填它。默认 null 而不是把明文塞进来，是为了让"忘了加密"这件事**表现为缺失**，
 *   而不是**表现为泄漏**。
 * @param latencyMs 已知延迟，通常为 null（尚未测速）。
 */
fun ShareNode.toProxyNode(
    id: String,
    sortIndex: Int = 0,
    credentialRef: String? = null,
    latencyMs: Int? = null,
): ProxyNode = ProxyNode(
    id = id,
    name = name.ifBlank { displayLabel },
    protocol = protocol ?: protocolFromRaw(protocolRaw),
    server = server,
    port = port,
    credentialRef = credentialRef,
    tls = toTlsOptions(),
    transport = toTransportOptions(),
    plugin = toPluginOptions(),
    tags = emptyList(),
    latencyMs = latencyMs,
    favorite = false,
    sortIndex = sortIndex,
    // rawJson 存**原始 URI**（含凭据）。
    // 这是刻意的：库里的 rawJson 是"导入来源的逐字留痕"，用于导出时还原与排障；
    // R5 要求的"不落明文"针对的是**结构化的凭据字段**，而 rawJson 的访问路径
    // 受数据库层保护（见 data 模块的加密与访问说明）。
    // 若把 rawJson 也抹掉，导入导出就不再是 round-trip，排障也无从下手。
    rawJson = raw,
)

/**
 * 由协议原文推导枚举。
 *
 * 为什么需要它：[ShareNode.protocol] 是可空的 —— 解析器在遇到**尚未支持**的协议时
 * 会把 `protocolRaw` 填好但留 `protocol` 为空，让上层能报出"不支持 X"而不是崩掉。
 * 这里做一次兜底映射，未知协议落到 [ProxyProtocol.SOCKS5]（与 data 层
 * `enumOr(ProxyProtocol.SOCKS5)` 的兜底保持一致，避免两处给出不同答案）。
 */
internal fun protocolFromRaw(raw: String): ProxyProtocol =
    when (raw.lowercase()) {
        "ss" -> ProxyProtocol.SS
        "ssr" -> ProxyProtocol.SSR
        "vmess" -> ProxyProtocol.VMESS
        "vless" -> ProxyProtocol.VLESS
        "trojan" -> ProxyProtocol.TROJAN
        "trojan-go" -> ProxyProtocol.TROJAN_GO
        "hysteria" -> ProxyProtocol.HYSTERIA
        "hysteria2", "hy2" -> ProxyProtocol.HYSTERIA2
        "tuic" -> ProxyProtocol.TUIC
        "http", "https" -> ProxyProtocol.HTTP
        "socks", "socks5" -> ProxyProtocol.SOCKS5
        else -> ProxyProtocol.SOCKS5
    }

/**
 * 组装 TLS 选项。
 *
 * 只在**确有必要**时返回非 null：`network=http` 这种明文传输返回 null，
 * 让"有没有 TLS"在数据里可区分，而不是到处挂一个全空的 TlsOptions。
 */
private fun ShareNode.toTlsOptions(): TlsOptions? {
    // 没有任何 TLS 痕迹时返回 null —— 空对象会让"未启用 TLS"与"启用了但没配参数"混同。
    if (!tls && sni.isBlank() && !insecure) return null
    return TlsOptions(
        // sni 为空时**不**用 host 顶替：两者语义不同（host 是 HTTP 头，sni 是握手扩展），
        // 顶替会让服务端看到错误的握手。宁可留 null 让内核用默认值。
        sni = sni.ifBlank { null },
        alpn = emptyList(),
        fingerprint = null,
        skipCertVerify = insecure,
        allowInsecure = insecure,
        realityPublicKey = null,
        realityShortId = null,
        pinnedSha256 = emptyList(),
    )
}

/**
 * 组装传输层选项。
 *
 * `network` 取值与 Go 侧一致：`tcp` / `ws` / `grpc` / `h2` / `quic` / `http` 等。
 * 未知取值一律落到 [TransportType.TCP]，**不抛异常** —— 一条订阅里出现厂商私有
 * 传输名是常态，为它整份订阅失败是不可接受的（与 [Subscription] 的部分成功语义一致）。
 */
private fun ShareNode.toTransportOptions(): TransportOptions? {
    val type = when (network.lowercase()) {
        "", "tcp" -> TransportType.TCP
        "ws" -> TransportType.WS
        "httpupgrade", "http_upgrade" -> TransportType.HTTP_UPGRADE
        "grpc" -> TransportType.GRPC
        "h2", "http2" -> TransportType.HTTP2
        "quic" -> TransportType.QUIC
        "mkcp", "kcp" -> TransportType.MKCP
        "http", "http-obfs", "obfs" -> TransportType.HTTP_OBFS
        else -> TransportType.TCP
    }
    // 全空且是 TCP 时返回 null：避免给每个普通节点都挂一个空 transport。
    if (type == TransportType.TCP && host.isBlank() && path.isBlank()) return null
    return TransportOptions(
        type = type,
        host = host.ifBlank { null },
        path = path.ifBlank { null },
        serviceName = null,
        headers = emptyMap(),
        earlyData = false,
        maxEarlyData = null,
    )
}

/**
 * 组装插件选项（`obfs` / `v2ray-plugin`）。
 *
 * 只有 [ShareNode.obfs] 非空才产出，且把 [ShareNode.obfsPassword] 一并带上 ——
 * ss 的 `obfs=http;obfs-password=xxx` 两个参数必须同时生效，只带一个等于配错。
 */
private fun ShareNode.toPluginOptions(): io.github.vipsvip.syfuse.domain.model.PluginOptions? {
    if (obfs.isBlank()) return null
    val opts = buildMap {
        if (obfsPassword.isNotBlank()) put("obfs-password", obfsPassword)
        if (host.isNotBlank()) put("host", host)
        if (path.isNotBlank()) put("path", path)
    }
    return io.github.vipsvip.syfuse.domain.model.PluginOptions(name = obfs, opts = opts)
}
