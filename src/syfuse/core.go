package syfuse

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Engine 是内核实例。
//
// gomobile 会把本类型导出成一个 Java 类，Kotlin 侧通过
// `io.github.vipsvip.syfuse.engine` 模块的 DataPlane 实现调用它。
//
// 并发约定：所有导出方法都加锁。gomobile 的 Java 侧可能从任意线程调用，
// 不加锁会在真实设备上表现为偶发 panic —— 那种 bug 极难复现。
type Engine struct {
	mu        sync.Mutex
	state     State
	config    *Config
	nodes     []Node
	startedAt time.Time
	stats     Stats
}

// State 是引擎状态。取值与 Kotlin 侧 EngineState 一一对应。
type State string

const (
	StateStopped     State = "stopped"
	StateStarting    State = "starting"
	StateRunning     State = "running"
	StateFailed      State = "failed"
	StateNotLinked   State = "not_linked"
	StateUnspecified State = ""
)

// Stats 是流量与连接计数。
type Stats struct {
	// 累计字节数。使用单调递增计数器，不做速率换算 ——
	// 速率是展示层的事（Kotlin 侧 TrafficMeter 负责 EMA），内核只报原始计数。
	TxBytes     int64 `json:"txBytes"`
	RxBytes     int64 `json:"rxBytes"`
	Connections int64 `json:"connections"`
	Blocked     int64 `json:"blocked"`
}

// NewEngine 创建一个未启动的内核实例。
func NewEngine() *Engine {
	return &Engine{state: StateNotLinked}
}

// Version 返回内核版本字符串。
func Version() string { return VersionConst }

// State 返回当前状态。
func (e *Engine) State() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == StateUnspecified {
		return string(StateNotLinked)
	}
	return string(e.state)
}

// Start 启动引擎。
//
// 当前行为（诚实声明）：
//  1. 解析并校验配置 —— 这一步是**真实执行**的，配置有问题会在这里被拒；
//  2. 解析节点 —— 同样真实执行；
//  3. 到"建立隧道"这一步返回 ErrTunnelNotImplemented。
//
// 也就是说 Start 会做完整的前置工作然后**如实失败**。
// 之所以不返回 nil：返回 nil 会让上层把状态置为"已连接"并显示给用户，
// 而实际没有任何流量被代理 —— 那是编造。
func (e *Engine) Start(configJSON string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state = StateStarting

	cfg, err := ParseConfig(configJSON)
	if err != nil {
		e.state = StateFailed
		return fmt.Errorf("配置校验失败: %w", err)
	}

	// 直连模式不需要节点，其余模式必须有
	if cfg.RoutingMode != RoutingModeDirect && len(cfg.Nodes) == 0 {
		e.state = StateFailed
		return fmt.Errorf("路由模式 %q 需要至少一个节点", cfg.RoutingMode)
	}

	nodes := make([]Node, 0, len(cfg.Nodes))
	for i, raw := range cfg.Nodes {
		n, perr := ParseNodeURI(raw)
		if perr != nil {
			e.state = StateFailed
			return fmt.Errorf("节点 #%d 解析失败: %w", i+1, perr)
		}
		nodes = append(nodes, n)
	}
	SortNodes(nodes)

	e.config = cfg
	e.nodes = nodes
	e.stats = Stats{}
	e.startedAt = time.Now()

	// 到这里为止都是真实工作。隧道本身尚未实现。
	e.state = StateNotLinked
	return fmt.Errorf("%s", ErrTunnelNotImplemented)
}

// Stop 停止引擎。因为从未真正启动隧道，这里只做状态归零。
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = StateStopped
	e.startedAt = time.Time{}
	return nil
}

// StatsJSON 返回统计信息的 JSON 表示。
//
// 返回 JSON 字符串而不是结构体：gomobile 对复杂返回类型支持有限，
// 字符串是最不容易出问题的边界类型。
func (e *Engine) StatsJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stats
	if !e.startedAt.IsZero() {
		s.Connections = int64(time.Since(e.startedAt).Seconds())
	}
	b, err := json.Marshal(s)
	if err != nil {
		return `{"error":"stats marshal failed"}`
	}
	return string(b)
}

// NodeCount 返回已解析的节点数。
func (e *Engine) NodeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.nodes)
}

// NodesJSON 返回节点列表（**不含凭据**），供 UI 展示。
func (e *Engine) NodesJSON() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return MarshalNodes(e.nodes)
}

// ParseSubscriptionJSON 解析订阅内容并返回节点 JSON（不含凭据）。
//
// 返回的第二个值是人类可读的逐行错误摘要；为空表示全部解析成功。
// 这是"部分成功"语义：坏行不会让整份订阅不可用。
func (e *Engine) ParseSubscriptionJSON(content string) (string, string, error) {
	nodes, errs := ParseSubscription(content)
	SortNodes(nodes)
	out, err := MarshalNodes(nodes)
	if err != nil {
		return "", "", err
	}
	if len(errs) == 0 {
		return out, "", nil
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return out, strings.Join(msgs, "; "), nil
}

// ParseNodesJSON 是 ParseSubscriptionJSON 的包级函数形式。
//
// 提供它是因为：解析订阅是**纯函数**，不需要先构造 Engine。
// 让调用方为了解析一个字符串而 new 一个引擎，是无谓的状态。
func ParseNodesJSON(content string) (string, string, error) {
	nodes, errs := ParseSubscription(content)
	SortNodes(nodes)
	out, err := MarshalNodes(nodes)
	if err != nil {
		return "", "", err
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return out, strings.Join(msgs, "; "), nil
}

// ValidateConfigJSON 校验配置并返回规范化后的 JSON。
//
// 提供给 Kotlin 侧在**保存配置之前**做一次校验，避免把坏配置写进数据库
// 等到连接时才发现。
func ValidateConfigJSON(configJSON string) (string, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
