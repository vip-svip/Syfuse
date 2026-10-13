package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/vip-svip/syfuse/src/internal/conn"
	"github.com/vip-svip/syfuse/src/internal/ipc"
	"github.com/vip-svip/syfuse/src/internal/leak"
	"github.com/vip-svip/syfuse/src/internal/netprobe"
	"github.com/vip-svip/syfuse/src/syfuse"
)

// proxyManager 是守护进程侧的代理状态机。
//
// 职责边界刻意很窄：**管理"选哪个节点、跑在哪份配置上、测速结果如何"**，
// 不碰真正的隧道建立（那是内核宿主的活，见 syfuse.Engine）。
// 这样拆的好处是：即使内核宿主还没接通，`proxies.status` 也能如实回答
// 当前选中与配置来源 —— 这些都是**真实状态**，不是占位。
type proxyManager struct {
	mu sync.Mutex

	// running 表示"用户已要求启动"。注意它**不等于**"流量已被代理"：
	// 后者取决于内核宿主是否接通，由 engineState 单独如实上报。
	running bool

	// profileID 是当前生效的配置档案 id。
	profileID string

	// selected 是 `proxies.select` 选中的节点名。
	selected string

	// lastTest 保存最近一次测速结果，按节点名索引。
	lastTest map[string]proxyTestResult

	// startedAt 是本次启动的时刻，用于 status 里报 uptime。
	startedAt time.Time

	// engine 是被驱动的真实引擎实例。
	//
	// 为 nil 表示尚未创建（还没启动过）。一旦 start 被调用，它就是
	// 真实的引擎：自带 SOCKS5 入站、路由决策与出站拨号器。
	// start 失败时状态如实置为 failed，**不会**假装 running。
	engine *syfuse.Engine
}

type proxyTestResult struct {
	// DelayMs 为 -1 表示"测速失败"，0 表示"尚未测速"。
	DelayMs int    `json:"delayMs"`
	Error   string `json:"error,omitempty"`
	At      int64  `json:"at"`
}

func newProxyManager() *proxyManager {
	return &proxyManager{lastTest: map[string]proxyTestResult{}}
}

// proxyStartParams 是 `proxies.start` / `proxies.reload` 的参数。
type proxyStartParams struct {
	// ConfigJSON 是配置包 JSON（与 Kotlin 侧 ConfigBundle 对应）。
	// 留空时从 syfuse.conf 推导出的路径读取。
	ConfigJSON string `json:"configJson"`

	// ProfileID 可选。
	ProfileID string `json:"profileId"`

	// Node 可选；指定初始选中节点。
	Node string `json:"node"`
}

// handleProxiesStart 处理 `proxies.start`。
func handleProxiesStart(pm *proxyManager, paths Paths) ipc.Handler {
	return func(params json.RawMessage) (any, error) {
		p, err := decodeProxyParams(params)
		if err != nil {
			return nil, err
		}
		configJSON := p.ConfigJSON
		if strings.TrimSpace(configJSON) == "" {
			configJSON, err = readActiveConfigJSON(paths)
			if err != nil {
				return nil, err
			}
		}
		return pm.start(configJSON, p.ProfileID, p.Node)
	}
}

func decodeProxyParams(params json.RawMessage) (proxyStartParams, error) {
	var p proxyStartParams
	if len(params) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return p, fmt.Errorf("参数解析失败: %w", err)
	}
	return p, nil
}

// readActiveConfigJSON 读取当前生效的配置包。
//
// 配置包路径来自 `syfuse.conf` 的 `CONFIG_PATH` 键（本产品新增键；
// 未设置时取 syfuse.conf 同目录下的 `config.json`）。
//
// 这一步是**真实 IO**：读不到就说读不到，不返回空配置让引擎"启动成功"。
func readActiveConfigJSON(paths Paths) (string, error) {
	kv, err := readSimpleConf(paths.ConfPath)
	if err != nil {
		return "", fmt.Errorf("读取 %s 失败: %w", paths.ConfPath, err)
	}
	ref := strings.TrimSpace(kv["CONFIG_PATH"])
	if ref == "" {
		// 未显式配置：用 conf 同目录的 config.json（约定位置）。
		ref = strings.TrimSuffix(paths.ConfPath, ".conf") + ".json"
	}
	b, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("读取配置包 %s 失败: %w", ref, err)
	}
	return string(b), nil
}

// readSimpleConf 读取 `KEY=value` 形态的配置。
//
// 约定（与 module/config/syfuse.conf 的头部注释一致）：
//   - `#` 开头为注释；
//   - 值不带引号，两侧空白裁掉；
//   - **未知键不报错**（前向兼容，6.4 要求 SY / SY-Fusion 键取并集）。
func readSimpleConf(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue // 非 KEY=value 形态：忽略而不是报错
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key != "" {
			out[key] = val
		}
	}
	return out, nil
}

// start 是启动的核心逻辑，独立出来便于测试直接驱动。
func (pm *proxyManager) start(configJSON, profileID, nodeName string) (any, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.engine == nil {
		pm.engine = syfuse.NewEngine()
	}

	// Start 成功时状态为 running，应答里带 socksAddr（本地入站地址）。
	// 失败时（配置坏、节点协议没有拨号器、端口被占用）状态为 failed，
	// 并把错误写进应答的 error 字段 —— 调用方必须能区分
	// "启动失败"与"以为启动成功了"。
	startErr := pm.engine.Start(configJSON)

	st := pm.engine.State()
	pm.running = st == string(syfuse.StateRunning) || st == string(syfuse.StateStarting)
	pm.startedAt = time.Now()
	if profileID != "" {
		pm.profileID = profileID
	}
	if nodeName != "" {
		pm.selected = nodeName
	} else if pm.selected == "" {
		if names := pm.nodeNamesLocked(); len(names) > 0 {
			pm.selected = names[0]
		}
	}

	resp := map[string]any{
		"state":        st,
		"running":      pm.running,
		"profileId":    pm.profileID,
		"selected":     pm.selected,
		"nodeCount":    pm.engine.NodeCount(),
		"kernelLinked": isLinked(st),
	}
	// socksAddr 是**真实存在**的本地入站地址：TUN/iptables 侧必须知道
	// 把流量送到哪里。端口可能是系统分配的，所以只能由引擎报回来，
	// 不能用配置里的字面量 —— 拿字面量去连会连到一个没人监听的端口。
	if addr := pm.engine.SocksAddr(); addr != "" {
		resp["socksAddr"] = addr
	}
	// 失败时把错误显式暴露。不再有"未接通但不当失败"那条分支 ——
	// 隧道已经实现，剩下的失败都是真失败（配置、协议、端口），
	// 留着一条永不命中分支只会让读者以为还有第三种可能。
	if startErr != nil {
		resp["error"] = startErr.Error()
	}
	return resp, nil
}

// isLinked 判断状态是否表示"内核宿主已接通"。
func isLinked(state string) bool {
	return state != string(syfuse.StateNotLinked) &&
		state != string(syfuse.StateUnspecified) &&
		state != string(syfuse.StateStopped)
}

// handleProxiesStop 处理 `proxies.stop`。
func handleProxiesStop(pm *proxyManager) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		pm.mu.Lock()
		defer pm.mu.Unlock()
		if pm.engine != nil {
			_ = pm.engine.Stop()
		}
		pm.running = false
		pm.startedAt = time.Time{}
		return map[string]any{"state": string(syfuse.StateStopped), "running": false}, nil
	}
}

// handleProxiesStatus 处理 `proxies`（等同 status）。
func handleProxiesStatus(pm *proxyManager) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		pm.mu.Lock()
		defer pm.mu.Unlock()

		state := string(syfuse.StateStopped)
		nodeCount := 0
		socksAddr := ""
		if pm.engine != nil {
			state = pm.engine.State()
			nodeCount = pm.engine.NodeCount()
			socksAddr = pm.engine.SocksAddr()
		}
		var uptime int64
		if !pm.startedAt.IsZero() {
			uptime = int64(time.Since(pm.startedAt).Seconds())
		}
		resp := map[string]any{
			"state":        state,
			"running":      pm.running,
			"profileId":    pm.profileID,
			"selected":     pm.selected,
			"nodeCount":    nodeCount,
			"uptimeSec":    uptime,
			"kernelLinked": isLinked(state),
		}
		if socksAddr != "" {
			resp["socksAddr"] = socksAddr
		}
		return resp, nil
	}
}

// handleProxiesReload 处理 `proxies.reload`：重读配置并按当前运行状态决定要不要重建隧道。
//
// 与 start 的区别有两条，都是有意的：
//  1. **未运行时只刷新节点表，不起隧道**（走 engine.Load）。若顺手起一条，
//     就会出现"守护进程里有个真监听端在收流量、UI 显示未连接"的分裂；
//  2. **运行中才重建隧道**（走完整 start）。换配置必须换隧道，
//     否则新规则/新节点永远不会生效，而 UI 显示的是新配置。
func (pm *proxyManager) reload(configJSON string, profileID, nodeName string) (any, error) {
	pm.mu.Lock()
	wasRunning := pm.running
	if pm.engine == nil {
		pm.engine = syfuse.NewEngine()
	}
	eng := pm.engine
	pm.mu.Unlock()

	if !wasRunning {
		// 只刷新节点表。错误必须让调用方看到 —— 静默保留旧节点表
		// 会让"reload 成功"与"配置其实没换"无法区分。
		if err := eng.Load(configJSON); err != nil {
			return nil, err
		}
		pm.mu.Lock()
		defer pm.mu.Unlock()
		if profileID != "" {
			pm.profileID = profileID
		}
		if nodeName != "" {
			pm.selected = nodeName
		}
		return map[string]any{
			"state":        eng.State(),
			"running":      false,
			"profileId":    pm.profileID,
			"selected":     pm.selected,
			"nodeCount":    eng.NodeCount(),
			"kernelLinked": isLinked(eng.State()),
			"reloaded":     true,
		}, nil
	}

	res, err := pm.start(configJSON, profileID, nodeName)
	if err != nil {
		return nil, err
	}
	m := res.(map[string]any)
	m["reloaded"] = true
	return m, nil
}

// handleProxiesReload 是 reload 的 IPC 包装：负责取出参数与配置来源。
func handleProxiesReload(pm *proxyManager, paths Paths) ipc.Handler {
	return func(params json.RawMessage) (any, error) {
		p, err := decodeProxyParams(params)
		if err != nil {
			return nil, err
		}
		configJSON := p.ConfigJSON
		if strings.TrimSpace(configJSON) == "" {
			configJSON, err = readActiveConfigJSON(paths)
			if err != nil {
				return nil, err
			}
		}
		return pm.reload(configJSON, p.ProfileID, p.Node)
	}
}

// handleProxiesSelect 处理 `proxies.select`。
//
// 只允许选**确实存在**的节点。允许选不存在的名字会让 UI 显示"已选中 X"，
// 而 X 根本不在节点表里 —— 那是编造状态。
func handleProxiesSelect(pm *proxyManager) ipc.Handler {
	return func(params json.RawMessage) (any, error) {
		var p struct {
			Node string `json:"node"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("参数解析失败: %w", err)
		}
		name := strings.TrimSpace(p.Node)
		if name == "" {
			return nil, fmt.Errorf("必须提供 node")
		}

		pm.mu.Lock()
		defer pm.mu.Unlock()

		names := pm.nodeNamesLocked()
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == name {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("节点 %q 不存在（当前共 %d 个候选）", name, len(names))
			}
		}
		pm.selected = name
		return map[string]any{"selected": pm.selected, "state": pm.stateLocked()}, nil
	}
}

// handleProxiesURLTest 处理 `proxies.urlTest`。
//
// **诚实声明**：真正的端到端测速需要内核宿主（要经隧道发请求）。
// 当前内核宿主未在进程内接通，因此这里做的是 **TCP 可达性预检**：
// 对每个节点的 server:port 发起一次带超时的 TCP 连接，成功即报实测毫秒数。
//
// 对无法连接的情形，返回 delayMs=-1 与原因，**不返回一个假的延迟数字**。
// 用户看到的"测速失败"必须是真失败。
func handleProxiesURLTest(pm *proxyManager) ipc.Handler {
	return func(params json.RawMessage) (any, error) {
		var p struct {
			Nodes   []string `json:"nodes"`
			Timeout int      `json:"timeoutMs"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("参数解析失败: %w", err)
			}
		}
		if p.Timeout <= 0 {
			p.Timeout = defaultURLTestTimeoutMs
		}

		targets, err := pm.resolveTestTargets(p.Nodes)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			return map[string]any{"results": []any{}, "note": "没有可测速的节点"}, nil
		}

		type outcome struct {
			name string
			res  proxyTestResult
		}
		results := make([]outcome, 0, len(targets))
		for _, t := range targets {
			res := netprobe.ProbeTCP(t.server, t.port, time.Duration(p.Timeout)*time.Millisecond)
			results = append(results, outcome{
				name: t.name,
				res: proxyTestResult{
					DelayMs: res.DelayMs,
					Error:   res.Error,
					At:      time.Now().Unix(),
				},
			})
		}

		pm.mu.Lock()
		for _, o := range results {
			pm.lastTest[o.name] = o.res
		}
		pm.mu.Unlock()

		out := make([]map[string]any, 0, len(results))
		for _, o := range results {
			m := map[string]any{"node": o.name, "delayMs": o.res.DelayMs}
			if o.res.Error != "" {
				m["error"] = o.res.Error
			}
			out = append(out, m)
		}
		return map[string]any{"results": out}, nil
	}
}

const defaultURLTestTimeoutMs = 3000

type testTarget struct {
	name   string
	server string
	port   int
}

// resolveTestTargets 把传入的节点名列表解析成可测速目标。
// 传入为空时对所有已知节点测速。
func (pm *proxyManager) resolveTestTargets(want []string) ([]testTarget, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.engine == nil {
		// 引擎还没起过：没有节点表可测。
		return nil, nil
	}
	// 从节点 JSON 读 server/port，而不是暴露内部切片：
	// 保证与 App 看到的是**同一份**（同一序列化路径，不会出现两套真相）。
	raw, err := pm.engine.NodesJSON()
	if err != nil {
		return nil, fmt.Errorf("读取节点表失败: %w", err)
	}
	nodes, err := syfuse.DecodeNodesJSONList(raw)
	if err != nil {
		return nil, fmt.Errorf("节点表解析失败: %w", err)
	}

	byName := make(map[string]testTarget, len(nodes))
	for _, n := range nodes {
		byName[n.Name] = testTarget{name: n.Name, server: n.Server, port: n.Port}
	}

	if len(want) == 0 {
		names := make([]string, 0, len(byName))
		for k := range byName {
			names = append(names, k)
		}
		sort.Strings(names)
		out := make([]testTarget, 0, len(names))
		for _, n := range names {
			out = append(out, byName[n])
		}
		return out, nil
	}

	out := make([]testTarget, 0, len(want))
	for _, n := range want {
		t, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("测速请求里的节点 %q 不存在", n)
		}
		out = append(out, t)
	}
	return out, nil
}

// nodeNamesLocked 返回已排序的节点名。调用方必须已持有 pm.mu。
func (pm *proxyManager) nodeNamesLocked() []string {
	if pm.engine == nil {
		return nil
	}
	raw, err := pm.engine.NodesJSON()
	if err != nil {
		return nil
	}
	nodes, err := syfuse.DecodeNodesJSONList(raw)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	return names
}

func (pm *proxyManager) stateLocked() string {
	if pm.engine == nil {
		return string(syfuse.StateStopped)
	}
	return pm.engine.State()
}

// ---------------------------------------------------------------------------
// connections / traffic —— M9 数据与统计的两个数据出口
// ---------------------------------------------------------------------------

// handleConnections 实现 M9 的 connections 方法：连接列表页的数据源。
//
// 数据来自引擎的 conn.Tracker（dataplane 在每次拨号成功时 Open、连接关闭
// 时按真实字节 Close —— 见 internal/dataplane 的 tracked()）。快照含
// 活动 + 已关闭记录，按开始时间序；字段名由 conn 包冻结（黄金值测试守着）。
//
// 引擎尚未创建时返回空记录而不是错误：界面此时该显示"没有数据"，
// 而不是把"还没启动引擎"渲染成一次接口故障。
func handleConnections(pm *proxyManager) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		pm.mu.Lock()
		defer pm.mu.Unlock()

		if pm.engine == nil {
			return map[string]any{"records": []any{}, "stats": conn.Stats{}}, nil
		}
		tr := pm.engine.Tracker()
		return map[string]any{"records": tr.Snapshot(), "stats": tr.Stats()}, nil
	}
}

// handleTraffic 实现 M9 的 traffic 方法：统计图表的数据源。
// 与 connections 严格同源（同一个 Tracker 的聚合视图），
// 口径不一致的"两份统计"比一份错的更难查。
func handleTraffic(pm *proxyManager) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		pm.mu.Lock()
		defer pm.mu.Unlock()

		if pm.engine == nil {
			return conn.Stats{}, nil
		}
		return pm.engine.Tracker().Stats(), nil
	}
}

// handleLeakCheck 实现 M12 的 leak.check 方法：一键防泄漏自检。
// 报告 JSON 直接来自 internal/leak 的 Report（字段冻结见该包）。
// 各维度的开关先全开；探测不可用的维度在报告里记 skip，
// 不让单点探测失败拖垮整个自检。
func handleLeakCheck() ipc.Handler {
	return func(json.RawMessage) (any, error) {
		probe := leak.NewRealProbe()
		checker, err := leak.NewChecker(probe)
		if err != nil {
			return nil, err
		}
		return checker.Run(leak.Config{
			CheckDNS: true, CheckIPv6: true, CheckSTUN: true, CheckQUIC: true,
			CheckIPEcho: true, CheckSplit: true, CheckSystem: true,
		}), nil
	}
}
