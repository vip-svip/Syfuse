// Package syfuse 是 Syfuse 的代理内核。
//
// 本包通过 `gomobile bind` 编译成 Android 可用的 AAR，由 Kotlin 侧的
// `io.github.vipsvip.syfuse.engine` 模块通过 DataPlane 接口调用。
//
// 当前实现范围（诚实声明，不夸大）：
//
//	已实现且可测：
//	  - 订阅内容解析（ss / ssr / vmess / vless / trojan / hysteria2 / http / socks）
//	  - 配置模型与校验
//	  - 节点去重与规范化
//	  - **本地入站隧道**：SOCKS5 监听 + 路由决策 + 出站拨号
//	    （ss / trojan / socks5 / http 上游，直连兜底）
//	  - 规则模式分流（与 Kotlin 侧同一套映射语义，见 tunnel.go）
//	  - 流量计数器（来自 dataplane 的真实计数）
//
//	未实现：
//	  - vmess / vless / ssr / hysteria2 的出站拨号器（URI 能解析，
//	    但没有对应拨号器，Start 会明确报错而不是回落直连）
//	  - UDP 转发（当前只做 TCP；DNS 走 UDP，属于 M6）
//	  - TUN 入站（由 Android 侧的 VpnService 负责，不在本包）
//
// 之所以把"未实现"写得这么显眼：一个返回 nil 却不做事的 Start()，
// 会让上层以为隧道已建立，进而显示"已连接"——那就是编造。
// 反过来说，**已实现的部分也绝不假装更完善**：拨号器不支持的协议
// 一律明确报错。
package syfuse

// VersionConst 是内核版本。
//
// 之所以叫 Const 而不是 Version：gomobile 导出的包级函数 `Version()` 会占用
// `Version` 这个标识符（导出为 Java 静态方法）。两者不能同名。
const VersionConst = "0.1.0"
