package rule

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// DomainTrie
// ---------------------------------------------------------------------------

func TestDomainTrie_SuffixMatchSelfAndSubdomainLongestFirst(t *testing.T) {
	trie := NewDomainTrie[string]()
	trie.Put("example.com", "wide")
	trie.Put("ads.example.com", "narrow")

	got := trie.Lookup("x.ads.example.com")
	want := []string{"narrow", "wide"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lookup = %v, want %v", got, want)
	}
}

func TestDomainTrie_LabelLevelMatchDoesNotOverreach(t *testing.T) {
	trie := NewDomainTrie[string]()
	trie.Put("example.com", "rule")

	if got := trie.Lookup("notexample.com"); len(got) != 0 {
		t.Errorf("notexample.com 不应命中，得到 %v", got)
	}
	if got := trie.Lookup("example.com.evil.test"); len(got) != 0 {
		t.Errorf("example.com.evil.test 不应命中，得到 %v", got)
	}
	if got := trie.Lookup("example.com"); !reflect.DeepEqual(got, []string{"rule"}) {
		t.Errorf("example.com 应命中 rule，得到 %v", got)
	}
}

func TestHostMatchesDomain_IsLabelLevel(t *testing.T) {
	cases := []struct {
		host, domain string
		want         bool
	}{
		{"a.example.com", "example.com", true},
		{"example.com", "example.com", true},
		{"notexample.com", "example.com", false},
		{"example.com.evil.io", "example.com", false},
		// 大小写与结尾点应被规范化
		{"A.Example.COM.", "example.com", true},
	}
	for _, c := range cases {
		if got := HostMatchesDomain(c.host, c.domain); got != c.want {
			t.Errorf("HostMatchesDomain(%q, %q) = %v, want %v", c.host, c.domain, got, c.want)
		}
	}
}

func TestDomainTrie_MultipleValuesOnSameDomain(t *testing.T) {
	trie := NewDomainTrie[int]()
	trie.Put("dup.test", 1)
	trie.Put("dup.test", 2)

	if got := trie.Lookup("dup.test"); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("lookup = %v, want [1 2]", got)
	}
}

// ---------------------------------------------------------------------------
// Ipv4CidrTrie
// ---------------------------------------------------------------------------

func TestIpv4CidrTrie_LongestPrefixMatch(t *testing.T) {
	trie := NewIpv4CidrTrie[string]()
	trie.Put("10.0.0.0/8", "wide")
	trie.Put("10.10.0.0/16", "narrow")

	if v, ok := trie.Lookup("10.10.5.5"); !ok || v != "narrow" {
		t.Errorf("10.10.5.5 = (%q,%v), want narrow", v, ok)
	}
	if v, ok := trie.Lookup("10.20.5.5"); !ok || v != "wide" {
		t.Errorf("10.20.5.5 = (%q,%v), want wide", v, ok)
	}
	if _, ok := trie.Lookup("192.168.1.1"); ok {
		t.Error("192.168.1.1 不应命中")
	}
}

func TestIpv4CidrTrie_RejectsInvalidAddressAndPrefix(t *testing.T) {
	trie := NewIpv4CidrTrie[string]()
	if trie.Put("999.1.1.1", "x") {
		t.Error("999.1.1.1 应被拒绝")
	}
	if trie.Put("1.2.3", "x") {
		t.Error("1.2.3 应被拒绝")
	}
	if trie.Put("1.2.3.4/33", "x") {
		t.Error("/33 应被拒绝")
	}
	if !trie.Put("1.2.3.4/24", "ok") {
		t.Fatal("1.2.3.4/24 应被接受")
	}
	if v, ok := trie.Lookup("1.2.3.4"); !ok || v != "ok" {
		t.Errorf("1.2.3.4 = (%q,%v), want ok", v, ok)
	}
	if _, ok := trie.Lookup("not-an-ip"); ok {
		t.Error("not-an-ip 不应命中")
	}
}

func TestIpv4CidrTrie_ZeroPrefixMatchesEverything(t *testing.T) {
	trie := NewIpv4CidrTrie[string]()
	trie.Put("0.0.0.0/0", "all")
	if v, ok := trie.Lookup("203.0.113.9"); !ok || v != "all" {
		t.Errorf("203.0.113.9 = (%q,%v), want all", v, ok)
	}
}

// ---------------------------------------------------------------------------
// CosmeticRule
// ---------------------------------------------------------------------------

func TestCosmeticRule_SixMarkersDistinguished(t *testing.T) {
	cases := []struct {
		line string
		want CosmeticType
	}{
		{"example.test##.ad", CosmeticHide},
		{"example.test#@#.ad", CosmeticUnhide},
		{"example.test#$#.ad { display:none }", CosmeticCSSInject},
		{"example.test#?#div:has(> .sponsor)", CosmeticExtendedCSS},
		{"example.test#%#//scriptlet('x')", CosmeticScriptlet},
		{"example.test##^script:has-text(ads)", CosmeticHTMLFilter},
	}
	for _, c := range cases {
		got := ParseCosmetic(c.line, 1, "test")
		if got == nil {
			t.Errorf("%q 应解析为元素规则", c.line)
			continue
		}
		if got.Type != c.want {
			t.Errorf("%q 类型 = %q, want %q", c.line, got.Type, c.want)
		}
	}
}

func TestCosmeticRule_HTMLFilterMarkerNotStolenByHide(t *testing.T) {
	r := ParseCosmetic("example.test##^script:has-text(ads)", 1, "test")
	if r == nil {
		t.Fatal("应解析成功")
	}
	if r.Body != "script:has-text(ads)" {
		t.Errorf("body = %q", r.Body)
	}
	if !reflect.DeepEqual(r.Scope.Include, []string{"example.test"}) {
		t.Errorf("include = %v", r.Scope.Include)
	}
}

func TestCosmeticRule_ScopeSupportsMultipleDomainsAndExclusion(t *testing.T) {
	r := ParseCosmetic("a.test,b.test,~c.a.test##.ad", 1, "test")
	if r == nil {
		t.Fatal("应解析成功")
	}
	if !reflect.DeepEqual(r.Scope.Include, []string{"a.test", "b.test"}) {
		t.Errorf("include = %v", r.Scope.Include)
	}
	if !reflect.DeepEqual(r.Scope.Exclude, []string{"c.a.test"}) {
		t.Errorf("exclude = %v", r.Scope.Exclude)
	}
	yes, no := "a.test", "c.a.test"
	other := "other.test"
	if !r.AppliesTo(&yes) {
		t.Error("a.test 应适用")
	}
	if r.AppliesTo(&no) {
		t.Error("c.a.test 应被排除")
	}
	if r.AppliesTo(&other) {
		t.Error("other.test 应不适用")
	}
}

func TestCosmeticRule_GlobalRuleHasNoDomainLimit(t *testing.T) {
	r := ParseCosmetic("##.ad-global", 1, "test")
	if r == nil {
		t.Fatal("应解析成功")
	}
	if !r.IsGlobal() {
		t.Error("应为全局规则")
	}
	anything := "anything.test"
	if !r.AppliesTo(&anything) {
		t.Error("全局规则应适用于任意域名")
	}
}

func TestCosmeticRule_NonCosmeticLinesReturnNil(t *testing.T) {
	if r := ParseCosmetic("||example.com^", 1, "test"); r != nil {
		t.Error("网络规则不应被解析为元素规则")
	}
	if r := ParseCosmetic("0.0.0.0 ads.test", 1, "test"); r != nil {
		t.Error("hosts 行不应被解析为元素规则")
	}
}

// ---------------------------------------------------------------------------
// RuleParser
// ---------------------------------------------------------------------------

func TestSplitPatternAndModifiers_RegexAnchorNotBroken(t *testing.T) {
	pattern, options := SplitPatternAndModifiers(`/ads\d+$/$script`)
	if pattern != `/ads\d+$/` {
		t.Errorf("pattern = %q", pattern)
	}
	if !reflect.DeepEqual(options, []string{"script"}) {
		t.Errorf("options = %v", options)
	}
}

func TestSplitPatternAndModifiers_PlainPattern(t *testing.T) {
	pattern, options := SplitPatternAndModifiers("||a.test^$important,script")
	if pattern != "||a.test^" {
		t.Errorf("pattern = %q", pattern)
	}
	if !reflect.DeepEqual(options, []string{"important", "script"}) {
		t.Errorf("options = %v", options)
	}
}

func TestSplitPatternAndModifiers_NoModifiers(t *testing.T) {
	pattern, options := SplitPatternAndModifiers("||a.test^")
	if pattern != "||a.test^" {
		t.Errorf("pattern = %q", pattern)
	}
	if len(options) != 0 {
		t.Errorf("options = %v, want empty", options)
	}
}

func TestParseLine_DomainModifierIncludeAndExclude(t *testing.T) {
	out := ParseLine("||a.test^$domain=b.test|~c.test", 7, "src", KindFilter)
	if out.Kind != OutcomeNetwork {
		t.Fatalf("kind = %v, want network", out.Kind)
	}
	scope := out.Rule.Mods.DomainScope
	if scope == nil {
		t.Fatal("domainScope 不应为 nil")
	}
	if !reflect.DeepEqual(scope.Include, []string{"b.test"}) {
		t.Errorf("include = %v", scope.Include)
	}
	if !reflect.DeepEqual(scope.Exclude, []string{"c.test"}) {
		t.Errorf("exclude = %v", scope.Exclude)
	}
}

func TestParseLine_ResourceTypesAccumulateAndAllExpands(t *testing.T) {
	two := ParseLine("||a.test^$script,image", 1, "", KindFilter)
	if two.Kind != OutcomeNetwork {
		t.Fatalf("kind = %v", two.Kind)
	}
	if !reflect.DeepEqual(two.Rule.Mods.ResourceTypes, []ResourceType{RTScript, RTImage}) {
		t.Errorf("types = %v", two.Rule.Mods.ResourceTypes)
	}

	all := ParseLine("||a.test^$all", 1, "", KindFilter)
	if len(all.Rule.Mods.ResourceTypes) != len(allResourceTypes) {
		t.Errorf("$all 应展开为 %d 项，得到 %d", len(allResourceTypes), len(all.Rule.Mods.ResourceTypes))
	}
}

func TestParseLine_XhrIsAliasOfXmlhttprequest(t *testing.T) {
	out := ParseLine("||a.test^$xhr", 1, "", KindFilter)
	if !reflect.DeepEqual(out.Rule.Mods.ResourceTypes, []ResourceType{RTXMLHTTPRequest}) {
		t.Errorf("types = %v", out.Rule.Mods.ResourceTypes)
	}
}

func TestParseLine_MatchCaseModifierReachesRegex(t *testing.T) {
	out := ParseLine("||a.test/Ads/$match-case", 1, "", KindFilter)
	if out.Kind != OutcomeNetwork {
		t.Fatalf("kind = %v", out.Kind)
	}
	re := out.Rule.Pattern.Regex
	if re == nil {
		t.Fatal("应为正则模式")
	}
	if !re.MatchString("https://a.test/Ads/x") {
		t.Error("应命中 https://a.test/Ads/x")
	}
	if re.MatchString("https://a.test/ads/x") {
		t.Error("match-case 下不应命中小写 ads")
	}
}

func TestParseLine_DefaultIsCaseInsensitive(t *testing.T) {
	out := ParseLine("||a.test/Ads/", 1, "", KindFilter)
	re := out.Rule.Pattern.Regex
	if re == nil {
		t.Fatal("应为正则模式")
	}
	if !re.MatchString("https://a.test/ads/x") {
		t.Error("默认应不区分大小写")
	}
}

func TestParseLine_BlankCommentAndHeaderIgnored(t *testing.T) {
	cases := []string{"   ", "! comment", "; comment", "# comment", "[Adblock Plus 2.0]"}
	for _, line := range cases {
		if got := ParseLine(line, 1, "", KindFilter); got.Kind != OutcomeIgnored {
			t.Errorf("%q 应被忽略，得到 kind=%v", line, got.Kind)
		}
	}
}

func TestParseLine_HostsLineMultipleHostnamesKeepsFirstAndWarns(t *testing.T) {
	out := ParseLine("0.0.0.0 a.test b.test", 3, "src", KindFilter)
	if out.Kind != OutcomeNetwork {
		t.Fatalf("kind = %v", out.Kind)
	}
	if out.Rule.Pattern.Domain != "a.test" {
		t.Errorf("domain = %q, want a.test", out.Rule.Pattern.Domain)
	}
	if len(out.Warnings) != 1 {
		t.Errorf("warnings = %d, want 1", len(out.Warnings))
	}
}

// ---------------------------------------------------------------------------
// 移植过程中发现的缺陷：两侧都必须守住
// ---------------------------------------------------------------------------

// TestPatternAnchorEnd 守住 `|...|` 结尾锚定。
//
// Kotlin 侧原实现把结尾锚点写成 `"\\$"`（正则里的 `\$`），而 `\$` 在
// Java 与 Go 中都是"字面美元符"，不是结尾锚点 —— 结果是所有
// `|https://example.com/|` 形态的规则**永远不可能命中**。
// 修复后 `$` 才是结尾锚点。本用例在修复前必然失败。
func TestPatternAnchorEnd(t *testing.T) {
	out := ParseLine("|https://anchor.test/x|", 1, "", KindFilter)
	if out.Kind != OutcomeNetwork {
		t.Fatalf("kind = %v, want network", out.Kind)
	}
	re := out.Rule.Pattern.Regex
	if re == nil {
		t.Fatal("应为正则模式")
	}
	if !re.MatchString("https://anchor.test/x") {
		t.Error("应命中完全一致的 URL")
	}
	if re.MatchString("https://anchor.test/x?q=1") {
		t.Error("结尾锚定下，带查询串的 URL 不应命中")
	}
	if re.MatchString("https://anchor.test/xy") {
		t.Error("结尾锚定下，更长路径不应命中")
	}
}

// TestCommentContainingHashHashIsIgnored 守住"# 注释"的判定。
//
// Kotlin 侧原实现按"能否解析出标记"区分注释与全局元素规则，
// 于是注释正文里只要出现 `##` 就会被当成元素规则。
// 修复后判据改为"标记必须在行首"。本用例在修复前必然失败。
func TestCommentContainingHashHashIsIgnored(t *testing.T) {
	line := "# 说明（与 ## 元素规则区分）"
	out := ParseLine(line, 1, "", KindFilter)
	if out.Kind != OutcomeIgnored {
		t.Fatalf("含 ## 的注释行应被忽略，得到 kind=%v", out.Kind)
	}
	// 真正的全局元素规则仍然必须被识别
	global := ParseLine("##.ad-global", 1, "", KindFilter)
	if global.Kind != OutcomeCosmetic {
		t.Fatalf("全局元素规则应被识别，得到 kind=%v", global.Kind)
	}
}

// TestHostsLineRecognizedBeforeDnsmasq 守住解析顺序：
// `address=/d/` 与 `0.0.0.0 d` 不能被互相误判。
func TestHostsLineRecognizedBeforeDnsmasq(t *testing.T) {
	a := ParseLine("address=/d.test/", 1, "", KindFilter)
	if a.Kind != OutcomeNetwork || a.Rule.Mods.DNSRewrite != nil {
		t.Errorf("address=/d.test/ 应为拦截（无 dnsRewrite），得到 kind=%v", a.Kind)
	}
	b := ParseLine("0.0.0.0 d.test", 1, "", KindFilter)
	if b.Kind != OutcomeNetwork || b.Rule.Mods.DNSRewrite != nil {
		t.Errorf("0.0.0.0 d.test 应为拦截，得到 kind=%v", b.Kind)
	}
	c := ParseLine("1.2.3.4 d.test", 1, "", KindFilter)
	if c.Kind != OutcomeNetwork || c.Rule.Mods.DNSRewrite == nil || *c.Rule.Mods.DNSRewrite != "1.2.3.4" {
		t.Errorf("1.2.3.4 d.test 应重写到 1.2.3.4")
	}
}

// ---------------------------------------------------------------------------
// 行数约定
// ---------------------------------------------------------------------------

// TestSplitLines_MatchesWcLineCount 守住行数约定。
//
// 判据是"行数 == 换行符个数"（`wc -l` 语义）。原 Kotlin 侧用
// `lineSequence()`，对以换行结尾的文本会多算一个空行，使规则体检的
// 行数比编辑器多 1；两侧现已统一为同一约定。
func TestSplitLines_MatchesWcLineCount(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"a\n", []string{"a"}},
		{"a", []string{"a"}},
		{"a\nb\n", []string{"a", "b"}},
		// 三个换行符 → 三行：a 加两个空行（只去掉一个结尾空段）
		{"a\n\n\n", []string{"a", "", ""}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"a\rb\r", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := splitLines(c.text)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitLines(%q) = %#v, want %#v", c.text, got, c.want)
		}
	}
	if got := splitLines(""); len(got) != 0 {
		t.Errorf("空文本应切成 0 行，得到 %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// FilterEngine 优先级模型（不依赖语料，独立守住 6.1 的裁决顺序）
// ---------------------------------------------------------------------------

func TestEngine_PriorityModel(t *testing.T) {
	src := "||block.test^\n" +
		"||imp-block.test^$important\n" +
		"@@||allow.test^\n" +
		"@@||imp-allow.test^$important\n" +
		"@@||tie.test^\n" +
		"||tie.test^\n"
	report := Parse(src, "unit", KindFilter)
	engine := Compile(report.NetworkRules, report.Diagnostics)

	cases := []struct {
		host     string
		decision Decision
		priority Priority
	}{
		{"block.test", DecisionBlock, PriorityNormalBlock},
		{"imp-block.test", DecisionBlock, PriorityImportantBlock},
		{"allow.test", DecisionAllow, PriorityNormalAllow},
		{"imp-allow.test", DecisionAllow, PriorityImportantAllow},
		// 同级下普通放行优先（因为它在候选里排在前，且 rank 相等时先到者胜）
		{"tie.test", DecisionAllow, PriorityNormalAllow},
		{"nothing.test", DecisionNone, PriorityNone},
	}
	for _, c := range cases {
		got := engine.Match(NewMatchRequest(c.host), true)
		if got.Decision != c.decision || got.Priority != c.priority {
			t.Errorf("%s → (%s,%s), want (%s,%s)",
				c.host, got.Decision, got.Priority.Name(), c.decision, c.priority.Name())
		}
	}
}

func TestEngine_BadFilterDisabledAtCompileTime(t *testing.T) {
	src := "||bad.test^\n||bad.test^$badfilter\n"
	report := Parse(src, "unit", KindFilter)
	engine := Compile(report.NetworkRules, report.Diagnostics)

	if engine.RuleCount() != 0 {
		t.Errorf("被 $badfilter 抵消后应剩 0 条，得到 %d", engine.RuleCount())
	}
	got := engine.Match(NewMatchRequest("bad.test"), false)
	if got.Decision != DecisionNone {
		t.Errorf("bad.test 应回到默认放行，得到 %s", got.Decision)
	}
	var audit int
	for _, d := range engine.Diagnostics {
		if d.Severity == SeverityInfo && containsString([]string{d.Message}, d.Message) &&
			len(d.Message) > 0 && d.Message[0] == '`' {
			audit++
		}
	}
	if audit == 0 {
		t.Error("应留下 $badfilter 审计诊断")
	}
}
