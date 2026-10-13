package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vip-svip/syfuse/src/syfuse/rule"
)

// 本文件实现 `rules.*` 控制方法与对应的 CLI 子命令。
//
// 它把 `src/syfuse/rule`（与 Kotlin 侧 core:rule 同语义的规则引擎）接到
// 守护进程上，使"内核宿主能拿到真实规则集"这件事成立 ——
// 在此之前，`rules.test` 只是一个返回"未实现（M5/M10）"的占位处理器。
//
// 诚实边界：
//   - 规则**语义**已实现且与 Kotlin 侧共用同一份语料与期望表（见 corpus_test.go）；
//   - 规则集的**来源管理**（订阅下载、自动更新、规则源 diff）仍属 M5/M10，
//     这里只读取本地已存在的规则文件，不联网、不下载；
//   - 编译产物落盘（`compiled.bin`）属 M5，本命令只做内存内编译与报告，
//     不写任何二进制文件 —— 不假装产出了不存在的产物。

// ruleFile 描述一个参与编译的规则文件。
type ruleFile struct {
	Name string
	Path string
	Kind rule.RuleKind
}

// ruleFiles 返回参与编译的规则文件清单。
//
// 为什么**不含** `lists.conf` / `sources.conf`：这两个文件的内容是
// "要订阅哪些规则源"的清单（URL 列表），不是规则本身。把它们当规则解析
// 会产生一堆无法编译的模式，然后以 WARNING 的形式淹没真正的诊断。
func ruleFiles(paths Paths) []ruleFile {
	return []ruleFile{
		{"user_rules", filepath.Join(paths.RulesDir, "user_rules.txt"), rule.KindFilter},
		{"blacklist", filepath.Join(paths.RulesDir, "blacklist.conf"), rule.KindBlock},
		// whitelist.conf 按**标准规则语法**解析：放行语义由 `@@` 前缀表达。
		// 提示词与 README 都只规定了这个文件"与 App 内可编辑项一一对应"，
		// 没有规定"非 @@ 行自动视为放行"。这里不自行发明语义 ——
		// 隐式白名单语义留待 M5 定稿，届时两侧一起改。
		{"whitelist", filepath.Join(paths.RulesDir, "whitelist.conf"), rule.KindFilter},
	}
}

// loadedRules 是一次规则集装载的结果。
type loadedRules struct {
	report  rule.Report
	files   []string // 实际读取到的文件（展示名）
	missing []string // 不存在（不是错误：规则目录可能本来就是空的）
}

// loadRulesFromFiles 读取规则目录并解析。
//
// 文件不存在不算错误：全新安装时规则目录是空的，此时引擎应当能正常编译出
// "0 条规则"，而不是报错。把 missing 单独返回，让调用方能如实展示。
func loadRulesFromFiles(paths Paths) loadedRules {
	var (
		network  []rule.CompiledNetworkRule
		cosmetic []rule.CosmeticRule
		diags    []rule.RuleDiagnostic
		files    []string
		missing  []string
		total    int
		ignored  int
	)
	for _, rf := range ruleFiles(paths) {
		b, err := os.ReadFile(rf.Path)
		if err != nil {
			missing = append(missing, rf.Name)
			continue
		}
		files = append(files, rf.Name)
		r := rule.Parse(string(b), rf.Name, rf.Kind)
		network = append(network, r.NetworkRules...)
		cosmetic = append(cosmetic, r.CosmeticRules...)
		diags = append(diags, r.Diagnostics...)
		total += r.TotalLines
		ignored += r.IgnoredLines
	}
	return loadedRules{
		report: rule.Report{
			NetworkRules:  network,
			CosmeticRules: cosmetic,
			Diagnostics:   diags,
			TotalLines:    total,
			IgnoredLines:  ignored,
		},
		files:   files,
		missing: missing,
	}
}

// loadRulesFromText 从给定文本解析（供 App 与测试传入临时规则集）。
func loadRulesFromText(text, sourceID string) loadedRules {
	if sourceID == "" {
		sourceID = "inline"
	}
	r := rule.Parse(text, sourceID, rule.KindFilter)
	return loadedRules{report: r, files: []string{sourceID}}
}

// pickRules 按"有内联文本就用内联，否则读目录"的规则装载。
func pickRules(paths Paths, inline *string) loadedRules {
	if inline != nil {
		return loadRulesFromText(*inline, "inline")
	}
	return loadRulesFromFiles(paths)
}

// ---------------------------------------------------------------------------
// JSON 视图
// ---------------------------------------------------------------------------

type ruleStatsJSON struct {
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
	Missing       []string `json:"missing,omitempty"`
}

func statsOf(lr loadedRules, engine *rule.FilterEngine) ruleStatsJSON {
	return ruleStatsJSON{
		Rules:         engine.RuleCount(),
		Indexed:       engine.IndexedRuleCount(),
		Linear:        engine.LinearRuleCount(),
		TotalLines:    lr.report.TotalLines,
		IgnoredLines:  lr.report.IgnoredLines,
		NetworkRules:  len(lr.report.NetworkRules),
		CosmeticRules: len(lr.report.CosmeticRules),
		Errors:        len(lr.report.Errors()),
		Warnings:      len(lr.report.Warnings()),
		Files:         lr.files,
		Missing:       lr.missing,
	}
}

type diagnosticJSON struct {
	Severity string `json:"severity"`
	LineNo   int    `json:"lineNo"`
	Raw      string `json:"raw"`
	Message  string `json:"message"`
	SourceID string `json:"sourceId,omitempty"`
}

func diagnosticsJSON(diags []rule.RuleDiagnostic) []diagnosticJSON {
	out := make([]diagnosticJSON, 0, len(diags))
	for _, d := range diags {
		out = append(out, diagnosticJSON{
			Severity: string(d.Severity),
			LineNo:   d.LineNo,
			Raw:      d.Raw,
			Message:  d.Message,
			SourceID: d.SourceID,
		})
	}
	return out
}

type chainEntryJSON struct {
	LineNo        int    `json:"lineNo"`
	Raw           string `json:"raw"`
	SourceID      string `json:"sourceId,omitempty"`
	Priority      string `json:"priority"`
	Action        string `json:"action"`
	Applied       bool   `json:"applied"`
	SkippedReason string `json:"skippedReason,omitempty"`
}

type matchedRuleJSON struct {
	LineNo   int    `json:"lineNo"`
	Raw      string `json:"raw"`
	SourceID string `json:"sourceId,omitempty"`
	Priority string `json:"priority"`
	Action   string `json:"action"`
	Kind     string `json:"kind"`
}

// rulesTestParams 是 rules.test 的参数。
//
// 字段名用小驼峰，与 UDS 上的其它方法（`lines`、`rotated` 等）保持一致。
type rulesTestParams struct {
	Rules        *string `json:"rules"`
	Host         string  `json:"host"`
	URL          *string `json:"url"`
	ResourceType string  `json:"resourceType"`
	DocumentHost *string `json:"documentHost"`
	Client       *string `json:"client"`
	Ctag         *string `json:"ctag"`
	Method       *string `json:"method"`
	ThirdParty   *bool   `json:"thirdParty"`
	CollectChain bool    `json:"collectChain"`
}

// handleRulesTest 实现 rules.test：对给定请求跑一次匹配并返回完整链路。
func handleRulesTest(paths Paths) func(json.RawMessage) (any, error) {
	return func(params json.RawMessage) (any, error) {
		var p rulesTestParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("参数解析失败: %w", err)
			}
		}

		host := p.Host
		if host == "" && p.URL != nil {
			host = rule.ExtractHost(*p.URL)
		}
		if host == "" {
			return nil, fmt.Errorf("缺少 host（或可从中推导 host 的 url）")
		}

		rt := rule.RTOther
		if p.ResourceType != "" {
			parsed, ok := rule.ResourceTypeFromToken(p.ResourceType)
			if !ok {
				return nil, fmt.Errorf("未知资源类型 %q；可用值：%s",
					p.ResourceType, strings.Join(rule.ResourceTypeTokens(), "/"))
			}
			rt = parsed
		}

		req := rule.MatchRequest{
			Host:         host,
			URL:          p.URL,
			ResourceType: rt,
			DocumentHost: p.DocumentHost,
			Client:       p.Client,
			Ctag:         p.Ctag,
			Method:       p.Method,
			ThirdParty:   p.ThirdParty,
		}

		lr := pickRules(paths, p.Rules)
		engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)
		result := engine.Match(req, p.CollectChain)

		chain := make([]chainEntryJSON, 0, len(result.Chain))
		for _, c := range result.Chain {
			e := chainEntryJSON{
				LineNo:   c.LineNo,
				Raw:      c.Raw,
				SourceID: c.SourceID,
				Priority: c.Priority.Name(),
				Action:   string(c.Action),
				Applied:  c.Applied,
			}
			if c.SkippedReason != nil {
				e.SkippedReason = *c.SkippedReason
			}
			chain = append(chain, e)
		}

		var matched *matchedRuleJSON
		if result.MatchedRule != nil {
			matched = &matchedRuleJSON{
				LineNo:   result.MatchedRule.LineNo,
				Raw:      result.MatchedRule.Raw,
				SourceID: result.MatchedRule.SourceID,
				Priority: result.MatchedRule.Priority().Name(),
				Action:   string(result.MatchedRule.Action()),
				Kind:     string(result.MatchedRule.Kind),
			}
		}

		return map[string]any{
			"decision": string(result.Decision),
			"priority": result.Priority.Name(),
			"blocked":  result.Blocked(),
			"allowed":  result.Allowed(),
			"matched":  matched,
			"chain":    chain,
			"stats":    statsOf(lr, engine),
		}, nil
	}
}

// handleRulesSyntax 实现 rules.syntax：返回逐行诊断（规则体检）。
func handleRulesSyntax(paths Paths) func(json.RawMessage) (any, error) {
	return func(params json.RawMessage) (any, error) {
		var p struct {
			Rules *string `json:"rules"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("参数解析失败: %w", err)
			}
		}
		lr := pickRules(paths, p.Rules)
		engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)
		return map[string]any{
			"stats":       statsOf(lr, engine),
			"diagnostics": diagnosticsJSON(lr.report.Diagnostics),
		}, nil
	}
}

// handleRulesStats 实现 rules.stats：只回计数。
func handleRulesStats(paths Paths) func(json.RawMessage) (any, error) {
	return func(params json.RawMessage) (any, error) {
		var p struct {
			Rules *string `json:"rules"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("参数解析失败: %w", err)
			}
		}
		lr := pickRules(paths, p.Rules)
		engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)
		return map[string]any{"stats": statsOf(lr, engine)}, nil
	}
}

// handleRulesBuild 实现 rules.build：编译并报告。
//
// **不写任何文件**。提示词里的 `compiled.bin` 属 M5；在此之前本方法
// 只做内存内编译并把结果如实报出来，不会留下一个"看起来编译过"的产物。
func handleRulesBuild(paths Paths) func(json.RawMessage) (any, error) {
	return func(params json.RawMessage) (any, error) {
		var p struct {
			Rules *string `json:"rules"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("参数解析失败: %w", err)
			}
		}
		lr := pickRules(paths, p.Rules)
		engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)
		return map[string]any{
			"stats": statsOf(lr, engine),
			"note": "本方法只在内存内编译并报告；编译产物（compiled.bin）落盘属 M5，" +
				"届时规则源管理与自动更新一并交付。",
		}, nil
	}
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

// cmdRulesTest 是 `syfused rules test <host> [资源类型]`。
func cmdRulesTest(paths Paths, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: syfused rules test <host|url> [资源类型]")
		return 2
	}
	value := args[0]
	rt := rule.RTOther
	if len(args) > 1 {
		parsed, ok := rule.ResourceTypeFromToken(args[1])
		if !ok {
			fmt.Fprintf(os.Stderr, "未知资源类型 %q；可用值：%s\n",
				args[1], strings.Join(rule.ResourceTypeTokens(), "/"))
			return 2
		}
		rt = parsed
	}

	lr := loadRulesFromFiles(paths)
	engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)

	req := rule.MatchRequest{ResourceType: rt}
	if strings.Contains(value, "://") {
		u := value
		req.URL = &u
		req.Host = rule.ExtractHost(value)
	} else {
		req.Host = value
	}

	result := engine.Match(req, true)
	fmt.Printf("请求      : %s（资源类型 %s）\n", value, rt)
	fmt.Printf("决策      : %s\n", result.Decision)
	fmt.Printf("优先级    : %s\n", result.Priority.Name())
	if result.MatchedRule != nil {
		m := result.MatchedRule
		fmt.Printf("命中规则  : [%s:%d] %s\n", m.SourceID, m.LineNo, m.Raw)
	} else {
		fmt.Println("命中规则  : 无（默认放行）")
	}
	fmt.Printf("候选链路  : %d 条\n", len(result.Chain))
	for _, c := range result.Chain {
		status := "未命中"
		if c.Applied {
			status = "命中"
		} else if c.SkippedReason != nil {
			status = "跳过：" + *c.SkippedReason
		}
		fmt.Printf("  %-4s [%s:%d] %s\n", status, c.SourceID, c.LineNo, c.Raw)
	}
	fmt.Printf("规则统计  : 总计 %d / 索引化 %d / 线性 %d\n",
		engine.RuleCount(), engine.IndexedRuleCount(), engine.LinearRuleCount())
	return 0
}

// cmdRulesReport 是 `syfused rules syntax|verify|build` 的共用实现。
//
// verify 与 syntax 的区别只在退出码：verify 在存在 ERROR 时返回 1，
// 便于 `action.sh` 与 CI 直接判断"规则集是否可用"。
func cmdRulesReport(paths Paths, sub string) int {
	lr := loadRulesFromFiles(paths)
	engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)

	fmt.Printf("规则文件  : %s\n", strings.Join(lr.files, ", "))
	if len(lr.missing) > 0 {
		fmt.Printf("未创建    : %s（不是错误：全新安装时规则目录为空）\n", strings.Join(lr.missing, ", "))
	}
	fmt.Printf("总行数    : %d（忽略 %d）\n", lr.report.TotalLines, lr.report.IgnoredLines)
	fmt.Printf("网络规则  : %d 条（索引化 %d / 线性 %d）\n",
		len(lr.report.NetworkRules), engine.IndexedRuleCount(), engine.LinearRuleCount())
	fmt.Printf("元素规则  : %d 条\n", len(lr.report.CosmeticRules))
	fmt.Printf("生效规则  : %d 条（%d 条被 $badfilter 关闭）\n",
		engine.RuleCount(), len(lr.report.NetworkRules)-engine.RuleCount())

	errors := lr.report.Errors()
	warnings := lr.report.Warnings()
	fmt.Printf("诊断      : ERROR %d / WARNING %d\n", len(errors), len(warnings))

	printDiags := func(label string, diags []rule.RuleDiagnostic) {
		if len(diags) == 0 {
			return
		}
		fmt.Printf("\n-- %s (%d) --\n", label, len(diags))
		for _, d := range diags {
			fmt.Printf("  [%s:%d] %s\n      %s\n", d.SourceID, d.LineNo, d.Raw, d.Message)
		}
	}
	printDiags("ERROR", errors)
	printDiags("WARNING", warnings)

	// 编译期信息（$badfilter 审计 + 编译摘要）也如实打出来。
	var infos []rule.RuleDiagnostic
	for _, d := range lr.report.Diagnostics {
		if d.Severity == rule.SeverityInfo {
			infos = append(infos, d)
		}
	}
	printDiags("INFO", infos)

	if sub == "build" {
		fmt.Println("\n注意：本命令只在内存内编译并报告；编译产物（compiled.bin）落盘属 M5。")
	}
	if sub == "verify" && len(errors) > 0 {
		fmt.Fprintf(os.Stderr, "\n规则集存在 %d 条 ERROR，判定为不可用。\n", len(errors))
		return 1
	}
	return 0
}

// ruleSourceNames 返回参与编译的文件名（供测试与文档使用）。
func ruleSourceNames(paths Paths) []string {
	names := make([]string, 0, 3)
	for _, rf := range ruleFiles(paths) {
		names = append(names, rf.Name)
	}
	sort.Strings(names)
	return names
}

// printRuleStats 打印规则集统计（供 `syfused rules stats` 使用）。
//
// 这里**真的编译一次**再报数，而不是打印"未实现" ——
// 规则引擎已经在 `src/syfuse/rule` 落地，继续声称"未实现"就是文档说谎。
func printRuleStats(paths Paths) {
	lr := loadRulesFromFiles(paths)
	engine := rule.Compile(lr.report.NetworkRules, lr.report.Diagnostics)
	fmt.Printf("规则文件  : %s\n", strings.Join(lr.files, ", "))
	if len(lr.missing) > 0 {
		fmt.Printf("未创建    : %s\n", strings.Join(lr.missing, ", "))
	}
	fmt.Printf("生效规则  : %d 条（索引化 %d / 线性 %d）\n",
		engine.RuleCount(), engine.IndexedRuleCount(), engine.LinearRuleCount())
	fmt.Printf("元素规则  : %d 条\n", len(lr.report.CosmeticRules))
	fmt.Printf("诊断      : ERROR %d / WARNING %d\n",
		len(lr.report.Errors()), len(lr.report.Warnings()))
}
