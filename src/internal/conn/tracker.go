// Package conn 实现守护进程的内核侧连接跟踪（M9 数据面）。
//
// 存在理由：daemon 的 proxies.status 一度只能报"假的连接数"，而 App 的
// M9 界面（连接列表 / 统计图表）需要真实数据。本包是 connections 与
// traffic 两个 UDS 方法的唯一数据源：代理内核每次连接建立调 Open，
// 连接关闭调 Close，界面按需 Snapshot / Stats。
//
// 硬约束（与 src/go.mod 一致）：只用标准库 —— 零第三方依赖、无 cgo、
// 无真实网络。本包是纯内存记账，网络语义由上层（socks5 / dataplane）
// 判定后告知本包。
//
// 内存模型（有界）：
//   - 记录数超过 MaxRecords 时淘汰**最旧已关闭**记录；
//   - 活动连接永不淘汰（容量是软上限，可被并发活动连接数突破）；
//   - 已关闭记录存放超过 Retention 后，在下次 Open/Close 时惰性淘汰。
//
// 一致性：Stats 的五个字段与 Snapshot 的当前内容严格一致
// （两者在同一把锁下取材），UDS 层可把 Stats 当作 Snapshot 的聚合。
package conn

import (
	"sort"
	"sync"
	"time"
)

// DefaultMaxRecords 是默认的环带容量上限。
// 取 10 万对齐 M9"10 万条无掉帧"的量级预期。
const DefaultMaxRecords = 100_000

// DefaultRetention 是已关闭连接的默认保留时长。
const DefaultRetention = 24 * time.Hour

// TrackerOptions 是 Tracker 的构造参数。零值全部字段都会走默认值。
type TrackerOptions struct {
	// MaxRecords 是环带容量上限（记录数）。<=0 用 DefaultMaxRecords。
	// 注意这是**软**上限：活动连接永不淘汰，全部活动时记录数可超过它。
	MaxRecords int

	// Now 是注入的时钟，nil 用 SystemClock（墙钟）。
	// 测试注入固定/可控时钟，使时间序断言与保留时长行为不依赖真实时间。
	// 约束：注入时钟不应返回零值 time.Time —— 零值被用作"未关闭"哨兵。
	Now func() time.Time

	// Retention 是已关闭记录的保留时长。<=0 用 DefaultRetention。
	// 超时记录在下次 Open/Close 时惰性淘汰（软保证：从最旧开始线性
	// 扫描，遇到未过期即停，故不保证精确按 EndTime 全局有序）。
	Retention time.Duration
}

// Tracker 是并发安全的连接跟踪器。
//
// 数据结构：map[uint64]*node 提供 O(1) 查找 + 双向链表维护开始时间序
// （即 ID 序，见 Open）。淘汰从链表头部进行，均摊 O(1)。
type Tracker struct {
	mu     sync.RWMutex
	byID   map[uint64]*node
	head   *node // 最旧
	tail   *node // 最新
	count  int   // len(byID)，当前被跟踪的记录数
	nextID uint64

	now    func() time.Time
	max    int
	retain time.Duration

	// 与 count / byID 在同一锁下维护的聚合计数，保证 Stats O(1)。
	activeN  int
	closedN  int
	bytesIn  int64
	bytesOut int64
}

// node 是链表节点，Record 内联存储避免二次分配。
type node struct {
	rec        Record
	prev, next *node
}

// NewTracker 创建 Tracker。零值 opts 合法（全部走默认）。
func NewTracker(opts TrackerOptions) *Tracker {
	t := &Tracker{
		byID:   make(map[uint64]*node),
		now:    opts.Now,
		max:    opts.MaxRecords,
		retain: opts.Retention,
	}
	if t.now == nil {
		t.now = SystemClock
	}
	if t.max <= 0 {
		t.max = DefaultMaxRecords
	}
	if t.retain <= 0 {
		t.retain = DefaultRetention
	}
	return t
}

// Open 登记一条新连接，返回分配的连接 ID（从 1 起单调递增）。
//
// info.ID 与 info.StartTime 会被忽略：ID 由本方法统一分配，StartTime
// 取当前时钟值（持写锁打时间戳，保证分配序即开始时间序的基础）。
// info.Labels 会被防御性拷贝，调用方之后修改原 map 不影响已记录的连接。
func (t *Tracker) Open(info ConnInfo) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextID++
	id := t.nextID
	n := &node{rec: Record{
		ConnInfo: ConnInfo{
			ID:        id,
			Protocol:  info.Protocol,
			SrcAddr:   info.SrcAddr,
			DstHost:   info.DstHost,
			DstPort:   info.DstPort,
			StartTime: t.now(),
			Labels:    copyLabels(info.Labels),
		},
	}}
	t.byID[id] = n
	t.pushBackLocked(n)
	t.count++
	t.activeN++
	t.evictLocked()
	return id
}

// Close 结束一条连接并记账。幂等：
//   - 未知 ID（含从未 Open、已被淘汰、拼写错误）安全忽略，不 panic；
//   - 同一 ID 第二次 Close 无效，不重复累计字节与已关闭数。
func (t *Tracker) Close(id uint64, bytesIn, bytesOut int64, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	n, ok := t.byID[id]
	if !ok {
		return // 未知 ID：安全忽略
	}
	rec := &n.rec
	if !rec.EndTime.IsZero() {
		return // 已关闭：幂等，不重复计数
	}
	rec.EndTime = t.now()
	rec.BytesIn = bytesIn
	rec.BytesOut = bytesOut
	rec.CloseReason = reason
	t.activeN--
	t.closedN++
	t.bytesIn += bytesIn
	t.bytesOut += bytesOut
	t.evictLocked()
}

// Snapshot 返回当前全部记录（活动 + 已关闭）的副本，按 (StartTime, ID)
// 升序。空 Tracker 返回空非 nil 切片。
//
// Labels 是浅拷贝：Open 时已做防御性拷贝且此后不再修改，共享安全。
func (t *Tracker) Snapshot() []Record {
	t.mu.RLock()
	out := make([]Record, 0, t.count)
	for n := t.head; n != nil; n = n.next {
		out = append(out, n.rec)
	}
	t.mu.RUnlock()

	// 链表序即插入序；时钟若被注入成非单调值，排序保证"按开始时间序"成立。
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartTime.Equal(out[j].StartTime) {
			return out[i].StartTime.Before(out[j].StartTime)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Stats 返回聚合统计。五个字段与 Snapshot 当前内容严格一致：
// Total == len(Snapshot)，Active + Closed == Total，
// 字节数为 Snapshot 内（含活动连接的 0 字节）Bytes 之和。
func (t *Tracker) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Stats{
		TotalConnections:  t.count,
		ActiveConnections: t.activeN,
		ClosedConnections: t.closedN,
		TotalBytesIn:      t.bytesIn,
		TotalBytesOut:     t.bytesOut,
	}
}

// evictLocked 执行淘汰。调用方必须持有写锁。
//
// 顺序：先容量淘汰（最旧已关闭先走），再保留时长淘汰（从头部线性扫）。
// 均摊 O(1)：容量淘汰每次 Open 最多触发一次、每次淘汰一条；扫描跳过
// 的只有活动连接，数量受并发连接数约束。
func (t *Tracker) evictLocked() {
	for t.count > t.max {
		victim := t.oldestClosedLocked()
		if victim == nil {
			break // 剩余全是活动连接，永不淘汰
		}
		t.removeLocked(victim)
	}
	now := t.now()
	for t.head != nil {
		rec := &t.head.rec
		if rec.EndTime.IsZero() || rec.EndTime.Add(t.retain).After(now) {
			break // 活动连接或未过期
		}
		t.removeLocked(t.head)
	}
}

// oldestClosedLocked 返回链表中开始时间最早的已关闭节点，没有则 nil。
func (t *Tracker) oldestClosedLocked() *node {
	for n := t.head; n != nil; n = n.next {
		if !n.rec.EndTime.IsZero() {
			return n
		}
	}
	return nil
}

// removeLocked 把节点从链表与索引中摘除，并同步聚合计数。
func (t *Tracker) removeLocked(n *node) {
	rec := &n.rec
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		t.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		t.tail = n.prev
	}
	n.prev, n.next = nil, nil
	delete(t.byID, rec.ID)
	t.count--
	t.closedN--
	t.bytesIn -= rec.BytesIn
	t.bytesOut -= rec.BytesOut
}

func (t *Tracker) pushBackLocked(n *node) {
	n.prev = t.tail
	if t.tail != nil {
		t.tail.next = n
	}
	t.tail = n
	if t.head == nil {
		t.head = n
	}
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
