package syfuse

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewEngine_InitialStateIsNotLinked(t *testing.T) {
	e := NewEngine()
	// 内核尚未接入时必须报 not_linked，而不是 stopped 或 running。
	// 这个区别对用户很重要：not_linked 表示"功能还没做"，
	// stopped 表示"做好了但你没开"。
	if got := e.State(); got != string(StateNotLinked) {
		t.Errorf("初始状态 = %q, 期望 %q", got, StateNotLinked)
	}
	if e.NodeCount() != 0 {
		t.Errorf("初始节点数 = %d, 期望 0", e.NodeCount())
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

func TestEngine_StartReportsTunnelNotImplemented(t *testing.T) {
	// 这是本项目最重要的一条诚实性测试：
	// Start 做了**真实的**配置校验与节点解析，然后在"建立隧道"这一步
	// **如实**返回未实现，而不是返回 nil 让上层显示"已连接"。
	e := NewEngine()
	err := e.Start(`{"schemaVersion":1,"routingMode":"rule","nodes":["ss://` +
		b64url("aes-256-gcm:pw") + `@1.1.1.1:8388#ok"]}`)

	if err == nil {
		t.Fatalf("隧道未实现时 Start 必须返回错误，否则上层会显示「已连接」——那是编造")
	}
	if !strings.Contains(err.Error(), "尚未实现") {
		t.Errorf("错误信息应明确说明未实现: %v", err)
	}
	if got := e.State(); got != string(StateNotLinked) {
		t.Errorf("状态 = %q, 期望 %q（不是 failed —— 这不是失败，是能力缺失）", got, StateNotLinked)
	}
	// 前置工作确实执行了：节点被解析并保存
	if e.NodeCount() != 1 {
		t.Errorf("节点数 = %d, 期望 1（说明 Start 确实做了真实的前置工作）", e.NodeCount())
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

func TestEngine_DirectModeWithoutNodesStillNotImplemented(t *testing.T) {
	// 直连模式不需要节点，因此会走过节点检查，仍然停在"隧道未实现"。
	// 这验证了校验顺序是正确的。
	e := NewEngine()
	err := e.Start(`{"schemaVersion":1,"routingMode":"direct"}`)
	if err == nil {
		t.Fatalf("隧道未实现，即使直连模式也应返回错误")
	}
	if !strings.Contains(err.Error(), "尚未实现") {
		t.Errorf("错误信息 = %v", err)
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
