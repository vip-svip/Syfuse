package conn

import "time"

// ConnInfo 描述一条连接的建立期信息（由代理内核在拨号成功时提供）。
//
// JSON 字段名是**冻结契约**：cmd/syfused 的 connections 方法直接把
// Record 序列化进 UDS 应答，Kotlin 侧连接列表页按这些名字取字段。
// 改名 = 破坏 App 兼容，必须先改两处再改这里。
type ConnInfo struct {
	// ID 连接标识，由 Tracker.Open 统一分配（从 1 起单调递增）。
	// Open 时传入的 info.ID 会被忽略。
	ID uint64 `json:"id"`

	// Protocol 传输层协议，如 "tcp"。内核目前只有 TCP 入站。
	Protocol string `json:"protocol"`

	// SrcAddr 客户端侧源地址，如 "127.0.0.1:52341"。
	SrcAddr string `json:"srcAddr"`

	// DstHost 目标主机（域名或 IP，来自 SOCKS5 请求）。
	DstHost string `json:"dstHost"`

	// DstPort 目标端口。
	DstPort int `json:"dstPort"`

	// StartTime 连接建立时间（Tracker 打点，调用方不可指定）。
	StartTime time.Time `json:"startTime"`

	// Labels 可选标签（如出站节点名、路由决策）。Open 时防御性拷贝。
	Labels map[string]string `json:"labels,omitempty"`
}

// Record 是一条连接的完整记录：ConnInfo + 关闭期信息。
//
// EndTime 零值表示连接仍活动（JSON 里序列化为 "0001-01-01T00:00:00Z"，
// 调用方（UDS 层）应先判 IsZero 再决定是否输出 endTime 字段）。
type Record struct {
	ConnInfo

	// EndTime 连接关闭时间；零值 = 仍活动。
	EndTime time.Time `json:"endTime"`

	// BytesIn 客户端 → 上游方向累计字节数（与 dataplane 的 TxBytes 同向）。
	BytesIn int64 `json:"bytesIn"`

	// BytesOut 上游 → 客户端方向累计字节数（与 dataplane 的 RxBytes 同向）。
	BytesOut int64 `json:"bytesOut"`

	// CloseReason 关闭原因，如 "client" / "upstream" / "shutdown" / "timeout"。
	// 活动连接为空串。
	CloseReason string `json:"closeReason,omitempty"`
}

// Closed 报告连接是否已关闭。
func (r Record) Closed() bool { return !r.EndTime.IsZero() }

// Stats 是 Tracker 的聚合统计。五个字段与 Snapshot 当前内容严格一致
// （同一把锁下取材）：Total == len(Snapshot)，Active + Closed == Total。
type Stats struct {
	TotalConnections  int   `json:"totalConnections"`
	ActiveConnections int   `json:"activeConnections"`
	ClosedConnections int   `json:"closedConnections"`
	TotalBytesIn      int64 `json:"totalBytesIn"`
	TotalBytesOut     int64 `json:"totalBytesOut"`
}
