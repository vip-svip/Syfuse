// Package netprobe 提供与网络可达性相关的最小探针。
//
// 存在的理由：`proxies.urlTest` 需要一个**真实的**可达性测量。
// 当前内核宿主尚未在守护进程内接通，无法经隧道测速，因此退一步做
// TCP 连接测量 —— 它测的是"这个 server:port 能否建连、花了多久"，
// 是一个真实且有意义的数，而不是编造出来的延迟。
//
// 本包**不依赖任何第三方库**（与全仓库一致），只用标准库。
package netprobe

import (
	"net"
	"strconv"
	"time"
)

// Result 是一次 TCP 探测的结果。
//
// DelayMs 约定：>=0 表示实测毫秒；-1 表示**失败**（此时 Error 非空）。
// 不用 0 表示失败：0 是一个合法的"极快"测量值，两者混用会让
// "测速失败"显示成"0 毫秒"。
type Result struct {
	DelayMs int
	Error   string
}

// ProbeTCP 对 host:port 发起一次带超时的 TCP 连接，返回建连耗时。
//
// 采用**已连接即关闭**（不做任何读写）：目的是测可达性与握手耗时，
// 不是探测服务类型。若目标端口是 TLS-only，这次连接同样会成功 ——
// 结果语义就是"TCP 层可达"，调用方不应把它当成"代理可用"。
func ProbeTCP(host string, port int, timeout time.Duration) Result {
	if host == "" {
		return Result{DelayMs: -1, Error: "节点缺少服务器地址"}
	}
	if port <= 0 || port > 65535 {
		return Result{DelayMs: -1, Error: "节点端口非法: " + strconv.Itoa(port)}
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return Result{DelayMs: -1, Error: classifyDialError(err, timeout)}
	}
	elapsed := time.Since(start)
	_ = conn.Close()

	ms := int(elapsed.Milliseconds())
	// 极快连接可能测出 0ms；用 1 表示"小于 1 毫秒"，
	// 避免与"未测速"的 0 混淆。
	if ms <= 0 {
		ms = 1
	}
	return Result{DelayMs: ms}
}

// classifyDialError 把人可读的原因归成几类。
//
// 不直接回传 err.Error()：它包含完整的 "dial tcp 1.2.3.4:443: ..." 前缀，
// 在 UI 上是一大段噪音；而用户真正要区分的是超时 / 拒绝 / 解析失败。
func classifyDialError(err error, timeout time.Duration) string {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "连接超时（" + timeout.String() + "）"
	}
	if dnsErr, ok := err.(*net.DNSError); ok {
		return "域名解析失败: " + dnsErr.Name
	}
	return "连接失败: " + err.Error()
}
