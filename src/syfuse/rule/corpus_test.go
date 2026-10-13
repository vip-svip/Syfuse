package rule

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// findRepoRoot 从当前测试目录向上找仓库根。
//
// 判据是"同时存在 settings.gradle.kts 与 tests/expect.tsv"。
// 找不到就**直接失败**，绝不静默跳过 —— 一个静默跳过的语料测试
// 会让"规则语义正确"这句话变成空话（这是本仓库既定的验证纪律）。
func findRepoRoot(t *testing.T) string {
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
			t.Fatalf("从 %s 向上未找到仓库根（需要同时存在 settings.gradle.kts 与 tests/expect.tsv）", dir)
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// loadCorpus 读取 tests/corpus 下全部 .txt，按文件名排序后拼接。
//
// 排序是必须的：语料可能被拆成多个文件，拼接顺序会影响行号与
// 规则插入顺序，进而影响"同优先级下先到者胜"的裁决结果。
func loadCorpus(t *testing.T) (Report, string) {
	t.Helper()
	root := findRepoRoot(t)
	corpusDir := filepath.Join(root, "tests", "corpus")

	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("读取语料目录失败: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s 下没有 .txt 语料", corpusDir)
	}
	sort.Strings(names)

	var parts []string
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(corpusDir, n))
		if err != nil {
			t.Fatalf("读取语料 %s 失败: %v", n, err)
		}
		parts = append(parts, string(b))
	}
	return Parse(strings.Join(parts, "\n"), "corpus", KindFilter), filepath.Join(root, "tests", "expect.tsv")
}

// loadExpectations 读取期望表，返回数据行（已剥离 BOM/CR、去掉注释与空行）。
func loadExpectations(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取期望表失败: %v", err)
	}
	raw := strings.Split(string(b), "\n")
	rows := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimPrefix(line, "\uFEFF")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// TestCorpusMatchesExpectations 是"规则语义正确"的主要可验证证据。
//
// 它与 Kotlin 侧 core/rule 的 CorpusConsistencyTest **读同一份语料与同一份
// 期望表**。两个语言实现若在语义上漂移，至少有一个会红。
func TestCorpusMatchesExpectations(t *testing.T) {
	report, expectPath := loadCorpus(t)
	engine := Compile(report.NetworkRules, report.Diagnostics)
	rows := loadExpectations(t, expectPath)
	if len(rows) == 0 {
		t.Fatal("期望表为空")
	}

	checked := 0
	for _, row := range rows {
		cols := strings.Split(row, "\t")
		if len(cols) < 8 {
			t.Fatalf("expect.tsv 列数不足 8：%q", row)
		}
		kind := strings.TrimSpace(cols[0])
		value := strings.TrimSpace(cols[1])
		rt, ok := ResourceTypeFromToken(strings.TrimSpace(cols[2]))
		if !ok {
			rt = RTOther
		}
		documentHost := optionalCol(cols[3])
		method := optionalCol(cols[4])
		wantDecision := strings.TrimSpace(cols[5])
		wantPriority := strings.TrimSpace(cols[6])
		note := strings.TrimSpace(cols[7])

		var req MatchRequest
		switch kind {
		case "host":
			req = MatchRequest{Host: value, ResourceType: rt, DocumentHost: documentHost, Method: method}
		case "url":
			u := value
			req = MatchRequest{
				Host: ExtractHost(value), URL: &u, ResourceType: rt,
				DocumentHost: documentHost, Method: method,
			}
		default:
			t.Fatalf("expect.tsv 中出现未知 kind：%q", kind)
		}

		got := engine.Match(req, true)
		if string(got.Decision) != wantDecision {
			t.Errorf("决策不符 → 输入 `%s`（%s）：得到 %s，期望 %s",
				value, note, got.Decision, wantDecision)
		}
		if got.Priority.Name() != wantPriority {
			t.Errorf("优先级不符 → 输入 `%s`（%s）：得到 %s，期望 %s",
				value, note, got.Priority.Name(), wantPriority)
		}
		checked++
	}

	// 这行输出与 Kotlin 侧 CorpusConsistencyTest 的打印格式对齐，
	// 便于把两个语言的数字直接对照（见 docs/EVIDENCE.md）。
	t.Logf("[CorpusConsistency/Go] 通过 %d 条期望；引擎规则数=%d，索引化=%d，线性=%d；"+
		"总行数=%d，忽略行数=%d，ERROR=%d，WARNING=%d",
		checked, engine.RuleCount(), engine.IndexedRuleCount(), engine.LinearRuleCount(),
		report.TotalLines, report.IgnoredLines, len(report.Errors()), len(report.Warnings()))
}

func optionalCol(s string) *string {
	v := strings.TrimSpace(s)
	if v == "-" || v == "" {
		return nil
	}
	return &v
}

func TestCorpus_InvalidRuleProducesLineNumberedError(t *testing.T) {
	report, _ := loadCorpus(t)
	errs := report.Errors()
	if len(errs) != 1 {
		t.Fatalf("应恰好 1 条 ERROR，得到 %d 条", len(errs))
	}
	e := errs[0]
	if e.LineNo <= 0 {
		t.Errorf("错误诊断必须带行号，得到 %d", e.LineNo)
	}
	if !strings.Contains(e.Raw, "||^") {
		t.Errorf("错误应指向 `||^` 所在行，得到 %q", e.Raw)
	}
	if e.Severity != SeverityError {
		t.Errorf("severity = %v", e.Severity)
	}
}

func TestCorpus_UnknownModifierProducesWarningNotSilence(t *testing.T) {
	report, _ := loadCorpus(t)
	var found bool
	for _, w := range report.Warnings() {
		if w.Raw == "||warn.test^$notamodifier" {
			found = true
		}
	}
	if !found {
		t.Error("未识别修饰符必须产出 WARNING，而不是静默丢弃")
	}
}

func TestCorpus_BadFilterEffectiveAndAudited(t *testing.T) {
	report, _ := loadCorpus(t)
	engine := Compile(report.NetworkRules, report.Diagnostics)

	var audit int
	for _, d := range engine.Diagnostics {
		if strings.Contains(d.Message, "$badfilter") {
			audit++
		}
	}
	if audit == 0 {
		t.Error("$badfilter 生效后必须留下审计诊断")
	}

	got := engine.Match(NewMatchRequest("bad.test"), false)
	if got.Decision != DecisionNone {
		t.Errorf("同形规则被关闭后 bad.test 应回到默认放行，得到 %s", got.Decision)
	}
}

func TestCorpus_CommentsAndHeadersIgnored(t *testing.T) {
	report, _ := loadCorpus(t)
	if report.IgnoredLines < 4 {
		t.Errorf("忽略行数应 >= 4，得到 %d", report.IgnoredLines)
	}
	for _, r := range report.NetworkRules {
		if strings.HasPrefix(r.Raw, "[") {
			t.Errorf("列表头不应产生规则：%q", r.Raw)
		}
	}
}

// TestCorpus_NoRuleSilentlyDropped 守住"语料里的规则要么生效、要么被明确诊断"。
//
// 这是防"假绿"的守卫：如果有人把解析器改坏，让一批规则被静默丢弃，
// 上面那些只看期望表的断言仍可能全绿（因为期望表只覆盖被查询的域名）。
func TestCorpus_NoRuleSilentlyDropped(t *testing.T) {
	report, _ := loadCorpus(t)
	// 语料里明确写出的、应当被解析为网络规则的条数。
	//
	// 这组数字由实测得出（探针 CorpusProbeTool），**不是人工点数**：
	// 早先靠人工点数得出 29，语料扩到 5 个文件后立刻失效，
	// 而"人工点数"本身也会数错。两边（Kotlin / Go）读同一份语料、
	// 断言同一组值，任一实现漂移都会红。
	const wantNetworkRules = 115
	if len(report.NetworkRules) != wantNetworkRules {
		t.Errorf("解析出的网络规则数 = %d，期望 %d。若确为语料变更请同步本常量；"+
			"否则说明有规则被静默丢弃或误判。", len(report.NetworkRules), wantNetworkRules)
	}
	// 元素类规则同理：语料含 6 条元素规则（HIDE/UNHIDE/CSS_INJECT/EXTENDED_CSS/
	// SCRIPTLET/HTML_FILTER 各一条）。
	//
	// 这一条同时守住"# 注释不得被误判为元素规则"：语料里有一条
	// 含 `##` 的注释，若判定退化成"正文里出现 ## 就算元素规则"，这里会变成 7。
	const wantCosmeticRules = 6
	if len(report.CosmeticRules) != wantCosmeticRules {
		t.Errorf("解析出的元素规则数 = %d，期望 %d", len(report.CosmeticRules), wantCosmeticRules)
	}
	// 忽略行与总行数也两侧对齐：总行数错会让"规则体检"的行号标注整体偏移。
	const wantIgnoredLines = 129
	if report.IgnoredLines != wantIgnoredLines {
		t.Errorf("忽略行数 = %d，期望 %d", report.IgnoredLines, wantIgnoredLines)
	}
	const wantTotalLines = 250
	if report.TotalLines != wantTotalLines {
		t.Errorf("总行数 = %d，期望 %d", report.TotalLines, wantTotalLines)
	}
}

// TestCorpus_HostsLineExpandsEveryHostname 守住 hosts 多主机名不静默丢弃。
//
// 旧实现只取第一个域名、其余仅发一条 WARNING —— 提示看着有，规则却真的没生效。
func TestCorpus_HostsLineExpandsEveryHostname(t *testing.T) {
	report, _ := loadCorpus(t)
	var domains []string
	for _, r := range report.NetworkRules {
		if r.Raw == "0.0.0.0 host-multi.test host-multi2.test" {
			domains = append(domains, r.Pattern.Domain)
		}
	}
	if len(domains) != 2 {
		t.Fatalf("多主机名 hosts 行应产出 2 条规则，得到 %d 条：%v", len(domains), domains)
	}
	if domains[0] != "host-multi.test" || domains[1] != "host-multi2.test" {
		t.Errorf("域名顺序不符：%v", domains)
	}
}

// TestCorpus_IPv6HostsBlackholeRecognized 守住 `::` / `::1` 不是死代码。
//
// blockIPs 里登记了 IPv6 黑洞地址，若 hosts 行正则只认 IPv4，
// 这两个条目永远走不到 —— "集合声称覆盖 IPv6" 就是假覆盖。
func TestCorpus_IPv6HostsBlackholeRecognized(t *testing.T) {
	report, _ := loadCorpus(t)
	for _, want := range []string{"host-v6any.test", "host-v6loop.test"} {
		var found bool
		for _, r := range report.NetworkRules {
			if r.Pattern.Domain == want {
				found = true
				if r.Mods.DNSRewrite != nil {
					t.Errorf("%s 是黑洞地址，不应产生 dnsRewrite（得到 %q）", want, *r.Mods.DNSRewrite)
				}
			}
		}
		if !found {
			t.Errorf("IPv6 黑洞行 `:: %s` / `::1 %s` 未被识别为规则", want, want)
		}
	}
}

// TestCorpus_DnsmasqServerDirectiveSupported 守住 dnsmasq `server=` 不被静默忽略。
func TestCorpus_DnsmasqServerDirectiveSupported(t *testing.T) {
	report, _ := loadCorpus(t)
	var found bool
	for _, r := range report.NetworkRules {
		if r.Pattern.Domain == "dq-d.test" {
			found = true
			if r.Mods.DNSRewrite == nil || *r.Mods.DNSRewrite != "1.1.1.1" {
				t.Errorf("`server=/dq-d.test/1.1.1.1` 应重写到 1.1.1.1，得到 %v", r.Mods.DNSRewrite)
			}
		}
	}
	if !found {
		t.Error("`server=/dq-d.test/1.1.1.1` 未被识别 —— dnsmasq server= 被静默忽略")
	}
}

// TestCorpus_MatchCaseReachesDomainAnchored 守住 `$match-case` 不被 Trie 吞掉。
//
// `||domain^` 走 Trie（键为 lower、判定不区分大小写），结构上无法表达
// "只匹配大写"。旧实现因此在 Trie 分支丢掉 matchCase，规则退化为不区分大小写。
func TestCorpus_MatchCaseReachesDomainAnchored(t *testing.T) {
	p := CompilePattern("||CaseSensitive.test^", true)
	if p == nil {
		t.Fatal("编译失败")
	}
	if p.Pattern.Kind != PatternRegex {
		t.Errorf("$match-case 的域名锚定规则必须退化为大小写敏感正则，得到 kind=%v", p.Pattern.Kind)
	}
	// 大小写敏感：只匹配原样写法
	if !p.Pattern.Regex.MatchString("https://CaseSensitive.test/x") {
		t.Error("应命中大小写完全一致的地址")
	}
	if p.Pattern.Regex.MatchString("https://casesensitive.test/x") {
		t.Error("$match-case 下不应命中小写写法")
	}
	// 对照：不带修饰符时仍然不区分大小写
	p2 := CompilePattern("||CaseSensitive.test^", false)
	if p2 == nil || p2.Pattern.Kind != PatternDomainOnly {
		t.Fatalf("无修饰符时应走 Trie 索引，得到 %v", p2)
	}
}
