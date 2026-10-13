package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件守住 `rules.*` 控制方法的**行为**（而不只是"注册了"）。
//
// 为什么需要它：Kotlin 侧的 DaemonProtocolContractTest 只核对
// "方法名/字段名/错误码有没有漂移"，它不关心处理器返回什么。
// 一个注册了但返回空对象、或把 decision 拼错的处理器，在那边是全绿的。

func tempPaths(t *testing.T) Paths {
	t.Helper()
	base := t.TempDir()
	p := DefaultPaths()
	p.DataDir = base
	p.RulesDir = filepath.Join(base, "rules")
	if err := os.MkdirAll(p.RulesDir, 0o700); err != nil {
		t.Fatalf("创建规则目录失败: %v", err)
	}
	return p
}

// rulesTestResult 是 rules.test 的应答视图。
type rulesTestResult struct {
	Decision string `json:"decision"`
	Priority string `json:"priority"`
	Blocked  bool   `json:"blocked"`
	Allowed  bool   `json:"allowed"`
	Matched  *struct {
		LineNo int    `json:"lineNo"`
		Raw    string `json:"raw"`
		Kind   string `json:"kind"`
	} `json:"matched"`
	Chain []struct {
		Raw           string `json:"raw"`
		Applied       bool   `json:"applied"`
		SkippedReason string `json:"skippedReason"`
	} `json:"chain"`
	Stats struct {
		Rules   int `json:"rules"`
		Indexed int `json:"indexed"`
		Linear  int `json:"linear"`
	} `json:"stats"`
}

func callRulesTest(t *testing.T, p Paths, params string) rulesTestResult {
	t.Helper()
	raw, err := handleRulesTest(p)(json.RawMessage(params))
	if err != nil {
		t.Fatalf("rules.test 返回错误: %v", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("应答无法序列化: %v", err)
	}
	var out rulesTestResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("应答无法反序列化: %v", err)
	}
	return out
}

func TestRulesTest_InlineRuleSetDecisions(t *testing.T) {
	p := tempPaths(t)
	inline := `||block.test^
@@||allow.test^
||imp.test^$important
@@||imp-allow.test^$important
||typed.test^$script`

	cases := []struct {
		name        string
		params      string
		decision    string
		priority    string
		wantChain   int
		wantMatched bool
	}{
		{"普通拦截", `{"rules":"` + esc(inline) + `","host":"block.test"}`, "block", "normal-block", 0, true},
		{"例外放行", `{"rules":"` + esc(inline) + `","host":"allow.test"}`, "allow", "normal-allow", 0, true},
		{"重要拦截", `{"rules":"` + esc(inline) + `","host":"imp.test"}`, "block", "important-block", 0, true},
		{"重要放行优先", `{"rules":"` + esc(inline) + `","host":"imp-allow.test"}`, "allow", "important-allow", 0, true},
		{"未命中默认放行", `{"rules":"` + esc(inline) + `","host":"nothing.test"}`, "none", "none", 0, false},
		// 资源类型不匹配 → 该规则被跳过，链路里必须能看到跳过原因
		{"资源类型不匹配", `{"rules":"` + esc(inline) + `","host":"typed.test","resourceType":"image","collectChain":true}`, "none", "none", 1, false},
		{"资源类型匹配", `{"rules":"` + esc(inline) + `","host":"typed.test","resourceType":"script"}`, "block", "normal-block", 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := callRulesTest(t, p, c.params)
			if got.Decision != c.decision {
				t.Errorf("decision = %q, want %q", got.Decision, c.decision)
			}
			if got.Priority != c.priority {
				t.Errorf("priority = %q, want %q", got.Priority, c.priority)
			}
			if (got.Matched != nil) != c.wantMatched {
				t.Errorf("matched 存在性 = %v, want %v", got.Matched != nil, c.wantMatched)
			}
			if got.Blocked != (c.decision == "block") {
				t.Errorf("blocked = %v 与 decision %q 不一致", got.Blocked, c.decision)
			}
			if got.Allowed != (c.decision == "allow") {
				t.Errorf("allowed = %v 与 decision %q 不一致", got.Allowed, c.decision)
			}
		})
	}
}

func TestRulesTest_ChainShowsSkipReason(t *testing.T) {
	p := tempPaths(t)
	got := callRulesTest(t, p,
		`{"rules":"||typed.test^$script","host":"typed.test","resourceType":"image","collectChain":true}`)
	if len(got.Chain) != 1 {
		t.Fatalf("链路应有 1 条候选，得到 %d", len(got.Chain))
	}
	if got.Chain[0].Applied {
		t.Error("资源类型不匹配的规则不应被标记为命中")
	}
	if !strings.Contains(got.Chain[0].SkippedReason, "资源类型不匹配") {
		t.Errorf("链路应给出跳过原因，得到 %q", got.Chain[0].SkippedReason)
	}
}

func TestRulesTest_DerivesHostFromURL(t *testing.T) {
	p := tempPaths(t)
	got := callRulesTest(t, p,
		`{"rules":"||path.test/ads/","url":"https://path.test/ads/x.js","resourceType":"script"}`)
	if got.Decision != "block" {
		t.Errorf("应从 URL 推导出 host 并命中，得到 %s", got.Decision)
	}
}

func TestRulesTest_RejectsBadInput(t *testing.T) {
	p := tempPaths(t)
	h := handleRulesTest(p)

	if _, err := h(json.RawMessage(`{"rules":"||a.test^"}`)); err == nil {
		t.Error("缺少 host/url 时应报错，而不是拿空 host 去匹配")
	}
	if _, err := h(json.RawMessage(`{"host":"a.test","resourceType":"不存在的类型"}`)); err == nil {
		t.Error("未知资源类型应报错，而不是静默退化成 other")
	}
	if _, err := h(json.RawMessage(`{`)); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

func TestRulesStats_LoadsFromRulesDir(t *testing.T) {
	p := tempPaths(t)
	// 语料原文作为规则文件；这样统计数字与 Go 侧语料测试完全一致。
	corpus := filepath.Join(repoRootForTest(t), "tests", "corpus", "01_syntax_mixed.txt")
	b, err := os.ReadFile(corpus)
	if err != nil {
		t.Fatalf("读取语料失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.RulesDir, "user_rules.txt"), b, 0o600); err != nil {
		t.Fatalf("写入规则文件失败: %v", err)
	}

	raw, err := handleRulesStats(p)(nil)
	if err != nil {
		t.Fatalf("rules.stats 返回错误: %v", err)
	}
	mb, _ := json.Marshal(raw)
	var out struct {
		Stats struct {
			Rules         int      `json:"rules"`
			Indexed       int      `json:"indexed"`
			Linear        int      `json:"linear"`
			TotalLines    int      `json:"totalLines"`
			IgnoredLines  int      `json:"ignoredLines"`
			NetworkRules  int      `json:"networkRules"`
			CosmeticRules int      `json:"cosmeticRules"`
			Errors        int      `json:"errors"`
			Warnings      int      `json:"warnings"`
			Files         []string `json:"files"`
			Missing       []string `json:"missing"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(mb, &out); err != nil {
		t.Fatalf("应答无法反序列化: %v", err)
	}

	// 这些数字必须与 core:rule 的 CorpusConsistencyTest 和 Go 侧
	// syfuse/rule 的语料测试给出的完全一致 —— 同一份语料，同一组数字。
	s := out.Stats
	if s.NetworkRules != 29 {
		t.Errorf("networkRules = %d, want 29", s.NetworkRules)
	}
	if s.CosmeticRules != 6 {
		t.Errorf("cosmeticRules = %d, want 6", s.CosmeticRules)
	}
	if s.TotalLines != 71 {
		t.Errorf("totalLines = %d, want 71", s.TotalLines)
	}
	if s.IgnoredLines != 35 {
		t.Errorf("ignoredLines = %d, want 35", s.IgnoredLines)
	}
	if s.Errors != 1 {
		t.Errorf("errors = %d, want 1", s.Errors)
	}
	if s.Warnings != 1 {
		t.Errorf("warnings = %d, want 1", s.Warnings)
	}
	// 29 条网络规则中，$badfilter 关掉 2 条 → 生效 27
	if s.Rules != 27 {
		t.Errorf("rules = %d, want 27", s.Rules)
	}
	if s.Indexed != 24 || s.Linear != 3 {
		t.Errorf("indexed/linear = %d/%d, want 24/3", s.Indexed, s.Linear)
	}
	if len(s.Files) != 1 || s.Files[0] != "user_rules" {
		t.Errorf("files = %v, want [user_rules]", s.Files)
	}
}

func TestRulesSyntax_ReportsLineNumberedDiagnostics(t *testing.T) {
	p := tempPaths(t)
	raw, err := handleRulesSyntax(p)(json.RawMessage(
		`{"rules":"||a.test^\n||^$script\n||b.test^$notamodifier"}`))
	if err != nil {
		t.Fatalf("rules.syntax 返回错误: %v", err)
	}
	mb, _ := json.Marshal(raw)
	var out struct {
		Diagnostics []struct {
			Severity string `json:"severity"`
			LineNo   int    `json:"lineNo"`
			Raw      string `json:"raw"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(mb, &out); err != nil {
		t.Fatalf("应答无法反序列化: %v", err)
	}
	var gotErr, gotWarn bool
	for _, d := range out.Diagnostics {
		switch d.Severity {
		case "error":
			gotErr = true
			if d.LineNo != 2 {
				t.Errorf("ERROR 行号 = %d, want 2", d.LineNo)
			}
		case "warning":
			gotWarn = true
			if d.LineNo != 3 {
				t.Errorf("WARNING 行号 = %d, want 3", d.LineNo)
			}
		}
	}
	if !gotErr || !gotWarn {
		t.Errorf("应同时产出 ERROR 与 WARNING 诊断（err=%v warn=%v）", gotErr, gotWarn)
	}
}

// TestRuleFiles_ExcludesSourceLists 守住"订阅源清单不是规则"这一判断。
//
// lists.conf / sources.conf 的内容是 URL 列表。把它们当规则解析会产出
// 一堆无法编译的模式，然后以 WARNING 淹没真正的诊断。
func TestRuleFiles_ExcludesSourceLists(t *testing.T) {
	p := tempPaths(t)
	names := ruleSourceNames(p)
	for _, n := range names {
		if n == "lists" || n == "sources" {
			t.Errorf("订阅源清单不应参与规则编译，却出现在 %v", names)
		}
	}
	if len(names) != 3 {
		t.Errorf("参与编译的规则文件应有 3 个，得到 %v", names)
	}
}

// repoRootForTest 向上找到仓库根（判据：settings.gradle.kts + tests/expect.tsv）。
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("无法获取工作目录: %v", err)
	}
	for {
		if fileExists(filepath.Join(dir, "settings.gradle.kts")) &&
			fileExists(filepath.Join(dir, "tests", "expect.tsv")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从 %s 向上未找到仓库根", dir)
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// esc 把规则文本转成可嵌入 JSON 字符串的形式。
func esc(s string) string {
	b, _ := json.Marshal(s)
	// 去掉首尾的引号，调用方自己加
	return string(b[1 : len(b)-1])
}
