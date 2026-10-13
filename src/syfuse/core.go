package syfuse

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vip-svip/syfuse/src/internal/conn"
	"github.com/vip-svip/syfuse/src/internal/dataplane"
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

	// tunnel 是正在运行的本地入站 + 出站隧道。nil 表示未启动。
	tunnel *dataplane.DataPlane

	// socksAddr 是 tunnel 实际监听到的地址（端口可能是系统分配的）。
	// 空串表示没有隧道在跑。由 [Engine.SocksAddr] 报给上层 ——
	// TUN/iptables 侧需要它来决定把流量送到哪里。
	socksAddr string

	// selectedNode 是下次启动时使用的节点名。空串表示用 nodes[0]。
	// 由 [Engine.SelectNode] 设置；运行中切换需要重启隧道，
	// 因此这里只记意图，实际生效点在 Start/Reload。
	selectedNode string

	// tracker 是连接跟踪器（M9）：connections/traffic 两个 UDS 方法的
	// 数据源。每个 Engine 一份、NewEngine 即创建 —— 即使从未 Start，
	// 接口也返回稳定的空记录而不是 nil（调用方无需判空）。
	tracker *conn.Tracker
}

// Tracker 返回本引擎的连接跟踪器。生命周期与 Engine 相同：
// 跨 Start/Stop 保留记录，重新 NewEngine 才清零。
func (e *Engine) Tracker() *conn.Tracker { return e.tracker }

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
//
// 初值是 stopped 而不是历史上的 not_linked：隧道已实现、内核就在本进程内，
// "未接通"这个概念已经没有对应事实了。留着 not_linked 当初值会让上层
// 把"还没点连接"显示成"内核没装上"——那是把能力缺失当成状态。
// StateNotLinked 常量本身保留（Kotlin 侧 EngineState 枚举与历史配置里
// 还有它），只是不再由本函数产生。
func NewEngine() *Engine {
	return &Engine{state: StateStopped, tracker: conn.NewTracker(conn.TrackerOptions{})}
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

// Start 启动引擎：校验配置 → 解析节点 → 建立并启动隧道。
//
// 三个步骤都是**真实执行**的，任何一步失败都会让 Start 返回错误，
// 且状态不会变成 running：
//  1. 配置校验 —— 配置有问题在这里被拒；
//  2. 节点解析 —— 坏 URI 在这里被拒并指出是第几个；
//  3. 隧道 —— 监听端口抢占失败、节点协议没有拨号器、规则模式却给空规则，
//     都在这里被拒。
//
// 成功时状态为 running，[Engine.SocksAddr] 返回实际监听地址。
//
// 为什么不再有"隧道尚未实现"那条路径：隧道已经实现（见 internal/dataplane），
// 保留一条永远走不到的占位分支只会让读者以为它还有可能发生。
// 但**失败必须如实失败**这一条没变 —— 不返回 nil 让上层显示"已连接"
// 而实际没有流量，那是编造。
func (e *Engine) Start(configJSON string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state = StateStarting

	if err := e.loadLocked(configJSON); err != nil {
		e.state = StateFailed
		return err
	}
	cfg, nodes := e.config, e.nodes

	e.stats = Stats{}
	e.startedAt = time.Now()

	// ---- 建立隧道 ----
	opts, err := newTunnelOptions(cfg, nodes)
	if err != nil {
		e.state = StateFailed
		return err
	}
	// 连接跟踪（M9）：每个 Engine 一份，跨 Start/Stop 保留记录
	// —— connections 接口报的是"本守护进程见过的连接"，不是"本次启动的"。
	opts.Tracker = e.tracker
	tun, err := dataplane.New(opts)
	if err != nil {
		e.state = StateFailed
		return fmt.Errorf("隧道构造失败: %w", err)
	}
	addr, err := tun.Start()
	if err != nil {
		// 监听失败（端口占用/权限）必须把半条隧道收干净，
		// 否则"Start 报错但端口还在听"。
		_ = tun.Close()
		e.state = StateFailed
		return fmt.Errorf("隧道启动失败: %w", err)
	}

	// 先停旧隧道再登记新的：重复 Start 不能留下两条隧道抢同一个入站。
	if e.tunnel != nil {
		_ = e.tunnel.Close()
	}
	e.tunnel = tun
	e.socksAddr = addr
	e.state = StateRunning
	return nil
}

// Load 解析配置与节点，但**不建立隧道**。
//
// 存在的唯一理由：`proxies.reload` 在引擎**未运行**时也要刷新节点表，
// 而"刷新节点表"与"启动隧道"必须分开。若 reload 顺手起了一条隧道，
// 就会出现"守护进程里有一个真监听端在收流量、UI 却显示未连接"——
// 这种分裂比报错难查十倍。
//
// Load 不改变 state、不动隧道、不动 startedAt：它只是把配置与节点表
// 换成新的。调用方（reload）据此如实回答未运行。
func (e *Engine) Load(configJSON string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.loadLocked(configJSON)
}

// loadLocked 是 Start 与 Load 共用的"解析并换配置"步骤。
// 调用方必须已持有 e.mu。
func (e *Engine) loadLocked(configJSON string) error {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return fmt.Errorf("配置校验失败: %w", err)
	}

	// 直连模式不需要节点，其余模式必须有
	if cfg.RoutingMode != RoutingModeDirect && len(cfg.Nodes) == 0 {
		return fmt.Errorf("路由模式 %q 需要至少一个节点", cfg.RoutingMode)
	}

	nodes := make([]Node, 0, len(cfg.Nodes))
	for i, raw := range cfg.Nodes {
		n, perr := ParseNodeURI(raw)
		if perr != nil {
			return fmt.Errorf("节点 #%d 解析失败: %w", i+1, perr)
		}
		nodes = append(nodes, n)
	}
	SortNodes(nodes)

	// 选中节点：默认排序后的第一个；[Engine.SelectNode] 指定的优先。
	nodes = orderSelectedFirst(nodes, e.selectedNode)

	e.config = cfg
	e.nodes = nodes
	return nil
}

// orderSelectedFirst 把选中的节点调到最前。
//
// 找不到选中节点时**保持原序**而不是报错：调用方（proxies.select）
// 已经校验过名字存在，这里再见到不存在的名字意味着配置在两次调用之间
// 变了；静默用第一个节点比让 Start 失败更合理 —— 节点表本身没坏。
func orderSelectedFirst(nodes []Node, selected string) []Node {
	if selected == "" || len(nodes) < 2 {
		return nodes
	}
	for i, n := range nodes {
		if n.Name == selected {
			out := make([]Node, 0, len(nodes))
			out = append(out, n)
			out = append(out, nodes[:i]...)
			out = append(out, nodes[i+1:]...)
			return out
		}
	}
	return nodes
}

// Stop 停止引擎：关闭隧道并归零状态。
//
// 隧道关闭是**真实**的：监听器关闭、在途连接收尾。返回 nil 表示
// "确实不再有隧道在跑"，调用方可以安全地认为 socksAddr 已失效。
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var err error
	if e.tunnel != nil {
		err = e.tunnel.Close()
		e.tunnel = nil
	}
	e.socksAddr = ""
	e.state = StateStopped
	e.startedAt = time.Time{}
	return err
}

// SocksAddr 返回本地入站的实际监听地址；未启动时返回空串。
//
// 端口可能是系统分配的（配置里写 0 或未配置），所以**必须**用这个值
// 而不是配置里的字面量 —— 拿字面量去连会连到一个没人监听的端口。
func (e *Engine) SocksAddr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.socksAddr
}

// SelectNode 选择下次启动使用的节点。
//
// 只记意图、不重启隧道：运行中切换节点要拆除当前隧道，
// 那会让在途连接全部断掉。真实客户端也是在切换后重连，由
// proxies.reload 触发重建。找不到这个名字时返回错误 ——
// 静默忽略会让 UI 显示"已选中 X"而内核用的是另一个节点。
func (e *Engine) SelectNode(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("节点名不能为空")
	}
	for _, n := range e.nodes {
		if n.Name == name {
			e.selectedNode = name
			return nil
		}
	}
	return fmt.Errorf("节点 %q 不存在（当前已解析 %d 个）", name, len(e.nodes))
}

// StatsJSON 返回统计信息的 JSON 表示。
//
// 数字来自隧道的真实计数器（见 dataplane.Stats），**不再**用
// "启动到现在的秒数"充当连接数 —— 那个数字与真实连接毫无关系，
// 用户看到"已连接 42 个"而实际可能一个都没有。 Connections 字段
// 仍然保留但语义改为"已建立的上游连接数"。
func (e *Engine) StatsJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	s := e.stats
	if e.tunnel != nil {
		ts := e.tunnel.Stats()
		s.Connections = ts.Connections
		s.TxBytes = ts.TxBytes
		s.RxBytes = ts.RxBytes
		s.Blocked = ts.Blocked
	} else if !e.startedAt.IsZero() && e.state == StateRunning {
		// 理论上不可达：running 状态一定有隧道。若真到这里，
		// 说明状态与隧道不一致，宁可全零也不编数字。
		s = Stats{}
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
//
// **签名受 gomobile 约束**：`gomobile bind` 只接受返回
// `(零个或一个值[, error])` 的导出函数，三返回值会让整份 AAR 构建失败
// （报 `too many result values`）。因此这里把它拆成两层：
//   - `parseSubscriptionJSONDetailed` 保留三返回值，供 Go 内部与测试使用；
//   - 本函数（导出的）返回 `(nodesJSON, err)`，逐行摘要并入 error，
//     保证**信息不丢**——调用方仍能拿到"哪几行坏了"。
func (e *Engine) ParseSubscriptionJSON(content string) (string, error) {
	out, summary, err := e.parseSubscriptionJSONDetailed(content)
	if err != nil {
		return "", err
	}
	if summary != "" {
		return out, fmt.Errorf("%s", summary)
	}
	return out, nil
}

// parseSubscriptionJSONDetailed 是三返回值形态，供 Go 内部与单元测试使用。
// 之所以保留：Go 测试需要区分"解析成功但有坏行"与"整体失败"两件事，
// 而导出给移动端的两返回值形态会把它们都塞进 error，类型上分不开。
func (e *Engine) parseSubscriptionJSONDetailed(content string) (string, string, error) {
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
	for _, er := range errs {
		msgs = append(msgs, er.Error())
	}
	return out, strings.Join(msgs, "; "), nil
}

// ParseNodesJSON 是 ParseSubscriptionJSON 的包级函数形式。
//
// 提供它是因为：解析订阅是**纯函数**，不需要先构造 Engine。
// 让调用方为了解析一个字符串而 new 一个引擎，是无谓的状态。
//
// 同 ParseSubscriptionJSON：导出版本必须是两返回值（gomobile 约束）。
func ParseNodesJSON(content string) (string, error) {
	out, summary, err := parseNodesJSONDetailed(content)
	if err != nil {
		return "", err
	}
	if summary != "" {
		return out, fmt.Errorf("%s", summary)
	}
	return out, nil
}

// parseNodesJSONDetailed 是三返回值形态，供 Go 内部与单元测试使用。
func parseNodesJSONDetailed(content string) (string, string, error) {
	nodes, errs := ParseSubscription(content)
	SortNodes(nodes)
	out, err := MarshalNodes(nodes)
	if err != nil {
		return "", "", err
	}
	msgs := make([]string, 0, len(errs))
	for _, er := range errs {
		msgs = append(msgs, er.Error())
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
