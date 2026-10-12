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
//	  - 流量计数器（用于 Stats 上报）
//
//	未实现：
//	  - 实际的数据面隧道（TCP/UDP 转发、TLS 握手、协议编解码）
//	    -> Start() 会返回 ErrTunnelNotImplemented，而不是假装连上
//
// 之所以把"未实现"写得这么显眼：一个返回 nil 却不做事的 Start()，
// 会让上层以为隧道已建立，进而显示"已连接"——那就是编造。
package syfuse

// VersionConst 是内核版本。
//
// 之所以叫 Const 而不是 Version：gomobile 导出的包级函数 `Version()` 会占用
// `Version` 这个标识符（导出为 Java 静态方法）。两者不能同名。
const VersionConst = "0.1.0"

// ErrTunnelNotImplemented 表示数据面隧道尚未实现。
//
// 这是一个**显式**错误，不是意外失败。上层收到它时应把引擎状态置为
// "内核未接入"，而不是"连接失败"——两者的用户含义完全不同。
const ErrTunnelNotImplemented = "syfuse: 数据面隧道尚未实现（内核处于骨架状态）"
