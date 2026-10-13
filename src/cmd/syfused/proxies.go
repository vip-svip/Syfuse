package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/vip-svip/syfuse/src/internal/ipc"
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
	// 为 nil 表示尚未创建。当前内核宿主（AAR）已可构建，但守护进程
	// 进程内还没加载它 —— 此时 start 会把状态如实标成 not_linked 并给出原因，
	// 而不是假装 running。
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

	// Start 在隧道未实现时会返回错误，但它**已经**完成了配置校验与节点解析。
	// 因此这里读状态而不是把错误直接抛出去 —— 抛出去会让"配置校验失败"
	// 与"内核未接通"混成同一个错误，调用方无法区分这两件事。
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
	// 只有在**确实失败**（而非"未接通"）时才把错误当成失败暴露。
	if startErr != nil {
		if st == string(syfuse.StateFailed) {
			resp["error"] = startErr.Error()
		} else {
			// 未接通：如实说明，但不当作失败。
			resp["note"] = startErr.Error()
		}
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
		if pm.engine != nil {
			state = pm.engine.State()
			nodeCount = pm.engine.NodeCount()
		}
		var uptime int64
		if !pm.startedAt.IsZero() {
			uptime = int64(time.Since(pm.startedAt).Seconds())
		}
		return map[string]any{
			"state":        state,
			"running":      pm.running,
			"profileId":    pm.profileID,
			"selected":     pm.selected,
			"nodeCount":    nodeCount,
			"uptimeSec":    uptime,
			"kernelLinked": isLinked(state),
		}, nil
	}
}

// handleProxiesReload 处理 `proxies.reload`：重读配置并重新解析节点。
//
// 与 start 的区别：reload **不改变**"运行与否"，只替换配置。
// 本来没跑就不因为 reload 变成在跑 —— 否则一次配置刷新会把 UI 变成"已连接"。
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

		pm.mu.Lock()
		wasRunning := pm.running
		pm.mu.Unlock()

		res, err := pm.start(configJSON, p.ProfileID, p.Node)
		if err != nil {
			return nil, err
		}

		pm.mu.Lock()
		pm.running = wasRunning
		pm.mu.Unlock()

		m := res.(map[string]any)
		m["reloaded"] = true
		return m, nil
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
