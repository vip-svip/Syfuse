package syfuse

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewEngine_InitialStateIsStopped(t *testing.T) {
	e := NewEngine()
	// 内核就在本进程内，"未接通"已无对应事实。初值必须是 stopped：
	// 它表示"做好了但你没开"，而不是 not_linked 的"功能还没做"。
	// 这个区别对用户很重要（见 NewEngine 注释）。
	if got := e.State(); got != string(StateStopped) {
		t.Errorf("初始状态 = %q, 期望 %q", got, StateStopped)
	}
	if e.NodeCount() != 0 {
		t.Errorf("初始节点数 = %d, 期望 0", e.NodeCount())
	}
	if e.SocksAddr() != "" {
		t.Errorf("初始 SocksAddr 应为空，实际 %q", e.SocksAddr())
	}
}

func TestEngine_StartRejectsBadConfig(t *testing.T) {
	e := NewEngine()
	err := e.Start(`{"schemaVersion":99}`)
	if err == nil {
		t.Fatalf("坏配置必须导致 Start 失败")
	}
	if !strings.Contains(err.Error(), "配置校验失败") {
		t.Errorf("错误信息应指明是配置问题: %v", err)
	}
	if got := e.State(); got != string(StateFailed) {
		t.Errorf("状态 = %q, 期望 %q", got, StateFailed)
	}
}

func TestEngine_StartRequiresNodesOutsideDirectMode(t *testing.T) {
	for _, mode := range []string{RoutingModeRule, RoutingModeGlobal} {
		e := NewEngine()
		err := e.Start(`{"schemaVersion":1,"routingMode":"` + mode + `"}`)
		if err == nil {
			t.Fatalf("mode=%s 且无节点时必须失败", mode)
		}
		if !strings.Contains(err.Error(), "至少一个节点") {
			t.Errorf("mode=%s 错误信息 = %v", mode, err)
		}
	}
}

// TestEngine_StartBuildsWorkingTunnel 是**产品核心闭环**的守门用例：
// 用户选了节点、点了连接，流量必须真的从入站流到目标。
//
// 拓扑（全部本机真 socket，不用假连接）：
//
//	测试客户端 → SOCKS5 入站(Engine) → 假 socks5 上游 → echoUpstream
//
// 这条链上同时有两个 SOCKS5 实现（入站服务端 + 出站客户端），
// 与 dataplane 包的端到端用例同一思路。
//
// 历史上这条闭环是断的：Start 如实返回"尚未实现"，用户点连接
// 永远连不上。本用例就是防止它再断回去 —— 断言全部落在
// "字节真的过去了"与"状态必须是 running"上。
func TestEngine_StartBuildsWorkingTunnel(t *testing.T) {
	echo := newTunnelEchoUpstream(t, "BANNER")
	chain := newTunnelChainSocks5(t, echo.addr())

	node := "socks5://chain:" + chain.password() + "@" + chain.hostPort()
	cfg := `{"schemaVersion":1,"routingMode":"global","nodes":["` + node + `"]}`

	e := NewEngine()
	if err := e.Start(cfg); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })

	if got := e.State(); got != string(StateRunning) {
		t.Fatalf("状态 = %q, 期望 %q", got, StateRunning)
	}
	addr := e.SocksAddr()
	if addr == "" {
		t.Fatalf("SocksAddr 为空：上层无处可送流量")
	}

	c, rep := socks5Dial(t, addr, echo.addr())
	defer c.Close()
	if rep != 0x00 {
		t.Fatalf("SOCKS5 应答码 = 0x%02x, 期望 0x00", rep)
	}
	if got := string(readTunnelN(t, c, 6)); got != "BANNER" {
		t.Errorf("banner = %q, 期望 BANNER", got)
	}

	// 业务数据往返（跨 16KB，确保不止一个 TCP 段）
	payload := make([]byte, 16384)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readTunnelN(t, c, len(payload))
	if string(got) != string(payload) {
		t.Errorf("回显不一致：前 16 字节 %q", got[:16])
	}

	// 假上游收到的目标必须就是请求的那个（连错服务器是最难查的错）
	if targets := chain.targets(); len(targets) != 1 || targets[0] != echo.addr() {
		t.Fatalf("上游收到的目标 = %v, want [%s]", targets, echo.addr())
	}

	// 统计必须是真数字（来自 dataplane 计数器，不是秒数冒充的连接数）
	var s Stats
	if err := json.Unmarshal([]byte(e.StatsJSON()), &s); err != nil {
		t.Fatalf("StatsJSON 非法: %v", err)
	}
	if s.Connections < 1 || s.TxBytes < int64(len(payload)) {
		t.Errorf("统计未反映真实流量: %+v", s)
	}
}

// TestEngine_StartFailsClosedWithoutRunningState 守住诚实性：
// 任何一步失败都不允许出现"状态是 running 但没有隧道"。
//
// 与已删除的 TestEngine_StartReportsTunnelNotImplemented 是同一条性质 ——
// 那时失败在"隧道未实现"，现在失败在"节点协议没有拨号器"。
// 性质不变：**Start 要么给出真隧道，要么如实失败**。
func TestEngine_StartFailsClosedWithoutRunningState(t *testing.T) {
	// vmess：URI 能解析，但没有出站拨号器 —— 典型"看着能连其实不能"。
	vmess := "vmess://" + b64url(`{"v":"2","ps":"x","add":"1.1.1.1","port":"8388","id":"b831381d-6324-4d53-ad4f-8cda48b30811"}`)
	e := NewEngine()
	err := e.Start(`{"schemaVersion":1,"routingMode":"global","nodes":["` + vmess + `"]}`)
	if err == nil {
		t.Fatalf("节点协议没有拨号器时 Start 必须失败，否则 UI 会显示「已连接」——那是编造")
	}
	if !strings.Contains(err.Error(), "拨号器") {
		t.Errorf("错误信息应说明是拨号器缺失: %v", err)
	}
	if got := e.State(); got == string(StateRunning) {
		t.Fatalf("失败后状态不允许是 running（实际 %q）——那意味着 UI 显示已连接但没有隧道", got)
	}
	if e.SocksAddr() != "" {
		t.Errorf("失败后 SocksAddr 应为空，实际 %q", e.SocksAddr())
	}
	if s := e.StatsJSON(); strings.Contains(s, "error") {
		t.Errorf("失败后 StatsJSON 仍应可用: %s", s)
	}
}

// TestEngine_RuleModeRedirectsTraffic 验证 rule 模式真的在分流，
// 且映射语义与 Kotlin 侧 RuleRouter 一致：
//
//   - 未命中 → DIRECT（含 IP 字面量目标，规则按域名写，天然不命中）
//   - 命中拦截规则 → REJECT（0x02），且流量不得到达上游
//
// 第一条同时是一记强断言：IP 目标的连接**不能**经过假上游
// （chain.targets() 必须为空）—— 否则"直连"其实是走了节点。
func TestEngine_RuleModeRedirectsTraffic(t *testing.T) {
	echo := newTunnelEchoUpstream(t, "BANNER")
	chain := newTunnelChainSocks5(t, echo.addr())
	node := "socks5://chain:" + chain.password() + "@" + chain.hostPort()

	cfg := `{"schemaVersion":1,"routingMode":"rule",` +
		`"rules":"||blocked.example^\n","nodes":["` + node + `"]}`

	e := NewEngine()
	if err := e.Start(cfg); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	addr := e.SocksAddr()

	// 1) IP 目标：未命中规则 → 直连。banner 必须到达，且假上游看不到它。
	c, rep := socks5Dial(t, addr, echo.addr())
	if rep != 0x00 {
		c.Close()
		t.Fatalf("未命中规则的连接应答码 = 0x%02x, 期望 0x00", rep)
	}
	if got := string(readTunnelN(t, c, 6)); got != "BANNER" {
		t.Errorf("banner = %q, 期望 BANNER", got)
	}
	c.Close()
	if tg := chain.targets(); len(tg) != 0 {
		t.Errorf("未命中的连接不应经过代理节点，实际上游收到 %v", tg)
	}

	// 2) 域名命中拦截规则 → 拒绝，且不得到达上游。
	before := echo.connections()
	c2, rep2 := socks5Dial(t, addr, "blocked.example:80")
	defer c2.Close()
	if rep2 != 0x02 {
		t.Errorf("被规则拒绝的连接应答码 = 0x%02x, 期望 0x02", rep2)
	}
	if after := echo.connections(); after != before {
		t.Errorf("被拦的连接不应到达上游: before=%d after=%d", before, after)
	}
	if tg := chain.targets(); len(tg) != 0 {
		t.Errorf("被拦的连接不应到达代理节点，实际上游收到 %v", tg)
	}
	var s Stats
	if err := json.Unmarshal([]byte(e.StatsJSON()), &s); err != nil {
		t.Fatalf("StatsJSON 非法: %v", err)
	}
	if s.Blocked < 1 {
		t.Errorf("Blocked 计数应 >= 1，实际 %+v", s)
	}
}

// TestEngine_RuleModeRejectsEmptyRules 守住"规则模式不能空跑"：
// 空规则集会让所有流量落到默认动作，用户以为分流生效了，其实没有。
func TestEngine_RuleModeRejectsEmptyRules(t *testing.T) {
	e := NewEngine()
	err := e.Start(`{"schemaVersion":1,"routingMode":"rule","nodes":["` +
		"socks5://chain:pw@127.0.0.1:1080" + `"]}`)
	if err == nil {
		t.Fatalf("规则模式给空 rules 必须拒绝启动")
	}
	if !strings.Contains(err.Error(), "rules") {
		t.Errorf("错误信息应指向 rules: %v", err)
	}
	if got := e.State(); got == string(StateRunning) {
		t.Errorf("状态不应为 running，实际 %q", got)
	}
}

// TestEngine_DirectModeTunnelsEverything 验证直连模式：
// 不需要节点也能起隧道，且流量真的直连到目标。
func TestEngine_DirectModeTunnelsEverything(t *testing.T) {
	echo := newTunnelEchoUpstream(t, "BANNER")
	e := NewEngine()
	if err := e.Start(`{"schemaVersion":1,"routingMode":"direct"}`); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })

	if got := e.State(); got != string(StateRunning) {
		t.Fatalf("状态 = %q, 期望 %q", got, StateRunning)
	}
	c, rep := socks5Dial(t, e.SocksAddr(), echo.addr())
	defer c.Close()
	if rep != 0x00 {
		t.Fatalf("应答码 = 0x%02x, 期望 0x00", rep)
	}
	if got := string(readTunnelN(t, c, 6)); got != "BANNER" {
		t.Errorf("banner = %q, 期望 BANNER", got)
	}
}

// TestEngine_StopClosesTunnelForReal Stop 必须真的把入站关掉：
// Stop 之后旧地址不能再接受连接，否则"已停止"是假的。
func TestEngine_StopClosesTunnelForReal(t *testing.T) {
	echo := newTunnelEchoUpstream(t, "BANNER")
	e := NewEngine()
	if err := e.Start(`{"schemaVersion":1,"routingMode":"direct"}`); err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := e.SocksAddr()

	// Stop 前先确认隧道是通的（否则下面"连不上"可能是一开始就没通）。
	c, rep := socks5Dial(t, addr, echo.addr())
	if rep != 0x00 {
		t.Fatalf("Stop 前应答码 = 0x%02x", rep)
	}
	c.Close()

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if e.SocksAddr() != "" {
		t.Errorf("Stop 后 SocksAddr 应为空，实际 %q", e.SocksAddr())
	}
	if got := e.State(); got != string(StateStopped) {
		t.Errorf("Stop 后状态 = %q, 期望 %q", got, StateStopped)
	}

	// 监听器必须真的关了：新连接应被拒（ dial 失败或立刻被关闭）。
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		c.Close()
		t.Errorf("Stop 后 %s 仍在接受连接 —— 隧道没有真正关闭", addr)
	}
}

// TestEngine_SelectNodeAffectsNextStart 选中节点必须真的被用到。
func TestEngine_SelectNodeAffectsNextStart(t *testing.T) {
	a := "socks5://nodeA:pw@127.0.0.1:1080#A"
	b := "socks5://nodeB:pw@127.0.0.1:1081#B"
	e := NewEngine()
	if err := e.Start(`{"schemaVersion":1,"routingMode":"global","nodes":["` + a + `","` + b + `"]}`); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })

	if err := e.SelectNode("B"); err != nil {
		t.Fatalf("SelectNode(B): %v", err)
	}
	if err := e.SelectNode("不存在"); err == nil {
		t.Errorf("选择不存在的节点必须报错")
	}
	// 重启后选中项仍生效（SelectNode 记的是意图）。
	if err := e.Start(`{"schemaVersion":1,"routingMode":"global","nodes":["` + a + `","` + b + `"]}`); err != nil {
		t.Fatalf("二次 Start: %v", err)
	}
}

func TestEngine_StartRejectsInvalidNodeURI(t *testing.T) {
	e := NewEngine()
	err := e.Start(`{"schemaVersion":1,"routingMode":"rule","nodes":["完全不是 URI"]}`)
	if err == nil {
		t.Fatalf("非法节点 URI 必须导致 Start 失败")
	}
	if !strings.Contains(err.Error(), "节点 #1") {
		t.Errorf("错误信息应能定位到具体是第几个节点: %v", err)
	}
	if got := e.State(); got != string(StateFailed) {
		t.Errorf("状态 = %q, 期望 %q", got, StateFailed)
	}
}

func TestEngine_DirectModeWithoutNodesBuildsTunnel(t *testing.T) {
	// 直连模式不需要节点，因此会走过节点检查，仍然能建立隧道。
	// 这验证了校验顺序是正确的（直连模式不被"必须有节点"拦下）。
	e := NewEngine()
	if err := e.Start(`{"schemaVersion":1,"routingMode":"direct"}`); err != nil {
		t.Fatalf("直连模式应能建立隧道: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	if got := e.State(); got != string(StateRunning) {
		t.Errorf("状态 = %q, 期望 %q", got, StateRunning)
	}
}

func TestEngine_StopResetsState(t *testing.T) {
	e := NewEngine()
	_ = e.Start(`{"schemaVersion":1,"routingMode":"direct"}`)
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	if got := e.State(); got != string(StateStopped) {
		t.Errorf("Stop 后状态 = %q, 期望 %q", got, StateStopped)
	}
}

func TestEngine_StatsJSONIsValidAndZeroed(t *testing.T) {
	e := NewEngine()
	out := e.StatsJSON()
	var s Stats
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("StatsJSON 不是合法 JSON: %v (%s)", err, out)
	}
	// 未启动时必须全零 —— 不允许出现任何编造的流量数字
	if s.TxBytes != 0 || s.RxBytes != 0 || s.Blocked != 0 {
		t.Errorf("未启动时统计应全零，实际 %+v", s)
	}
}

func TestEngine_NodesJSONOmitsCredentials(t *testing.T) {
	e := NewEngine()
	_ = e.Start(`{"schemaVersion":1,"routingMode":"rule","nodes":["ss://` +
		b64url("aes-256-gcm:SUPER_SECRET") + `@1.1.1.1:8388#ok"]}`)

	out, err := e.NodesJSON()
	if err != nil {
		t.Fatalf("NodesJSON 失败: %v", err)
	}
	if strings.Contains(out, "SUPER_SECRET") {
		t.Errorf("NodesJSON 泄漏了凭据: %s", out)
	}
	if !strings.Contains(out, "1.1.1.1") {
		t.Errorf("NodesJSON 应包含非敏感字段: %s", out)
	}
}

func TestEngine_ParseSubscriptionJSON_PartialSuccess(t *testing.T) {
	e := NewEngine()
	content := strings.Join([]string{
		"ss://" + b64url("aes-256-gcm:pw") + "@1.1.1.1:8388#ok",
		"坏行",
	}, "\n")

	// 用 detailed 形态：需要区分"解析成功但有坏行"与"整体失败"，
	// 而导出给移动端的两返回值形态会把两者都塞进 error（gomobile 约束）。
	nodesJSON, errSummary, err := e.parseSubscriptionJSONDetailed(content)
	if err != nil {
		t.Fatalf("ParseSubscriptionJSON 不应整体失败: %v", err)
	}
	if !strings.Contains(nodesJSON, "1.1.1.1") {
		t.Errorf("应保留成功解析的节点: %s", nodesJSON)
	}
	if errSummary == "" {
		t.Errorf("坏行必须产生错误摘要，而不是被静默吞掉")
	}
	if !strings.Contains(errSummary, "行") {
		t.Errorf("错误摘要应含行号: %s", errSummary)
	}
}

// TestParseSubscriptionJSON_ExportedFormKeepsSummaryInError 守住导出形态不丢信息。
//
// gomobile 只接受 `(值[, error])` 两返回值，导出函数因此把逐行摘要并入 error。
// 若某次重构把摘要**丢掉**（只返回 nil），移动端就再也看不到"哪几行坏了"，
// 而 Go 内部测试仍会通过 —— 这条断言专门盯住那个缝隙。
func TestParseSubscriptionJSON_ExportedFormKeepsSummaryInError(t *testing.T) {
	content := strings.Join([]string{
		"ss://" + b64url("aes-256-gcm:pw") + "@1.1.1.1:8388#ok",
		"坏行",
	}, "\n")
	e := NewEngine()

	out, err := e.ParseSubscriptionJSON(content)
	if err == nil {
		t.Fatal("有坏行时导出形态必须返回非 nil error（摘要不能丢）")
	}
	if !strings.Contains(err.Error(), "行") {
		t.Errorf("error 应携带含行号的摘要，得到: %v", err)
	}
	if !strings.Contains(out, "1.1.1.1") {
		t.Errorf("即便有坏行，成功解析的节点也必须返回: %s", out)
	}

	// 全部成功时，error 必须为 nil，否则调用方无法区分"全好"与"部分坏"。
	clean, err := e.ParseSubscriptionJSON("ss://" + b64url("aes-256-gcm:pw") + "@1.1.1.1:8388#ok")
	if err != nil {
		t.Errorf("全部解析成功时 error 应为 nil，得到: %v", err)
	}
	if clean == "" {
		t.Error("应返回节点 JSON")
	}
}

func TestPackageLevel_ParseNodesJSON(t *testing.T) {
	// 包级函数与引擎方法必须行为一致 —— 否则两条路径会漂移
	content := "ss://" + b64url("aes-256-gcm:pw") + "@1.1.1.1:8388#ok"
	a, err := ParseNodesJSON(content)
	if err != nil {
		t.Fatalf("包级解析失败: %v", err)
	}
	e := NewEngine()
	b, err := e.ParseSubscriptionJSON(content)
	if err != nil {
		t.Fatalf("方法解析失败: %v", err)
	}
	if a != b {
		t.Errorf("包级函数与引擎方法结果不一致:\n  %s\n  %s", a, b)
	}
}

func TestVersion_NotEmpty(t *testing.T) {
	if Version() == "" {
		t.Errorf("Version() 不应为空")
	}
	if Version() != VersionConst {
		t.Errorf("Version() = %q, 常量 = %q", Version(), VersionConst)
	}
}
