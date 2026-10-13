package rule

import "strings"

// OutcomeKind 区分单行解析结果。
type OutcomeKind int

const (
	// OutcomeNetwork 表示成功解析出一条网络规则。
	OutcomeNetwork OutcomeKind = iota
	// OutcomeCosmetic 表示成功解析出一条元素隐藏/脚本注入类规则。
	OutcomeCosmetic
	// OutcomeIgnored 表示空行、注释、头部声明等无需处理的行。
	OutcomeIgnored
	// OutcomeInvalid 表示解析失败。
	OutcomeInvalid
)

// ParseOutcome 是单行解析结果。
type ParseOutcome struct {
	Kind OutcomeKind

	// OutcomeNetwork 使用。
	Rule     *CompiledNetworkRule
	Warnings []RuleDiagnostic

	// OutcomeCosmetic 使用。
	Cosmetic *CosmeticRule

	// OutcomeIgnored 使用。
	IgnoredReason string

	// OutcomeInvalid 使用。
	Diagnostic *RuleDiagnostic
}

// Report 是一次解析的汇总结果。
type Report struct {
	NetworkRules  []CompiledNetworkRule
	CosmeticRules []CosmeticRule
	Diagnostics   []RuleDiagnostic
	TotalLines    int
	IgnoredLines  int
}

// Errors 返回全部 ERROR 级诊断。
func (r Report) Errors() []RuleDiagnostic {
	var out []RuleDiagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// Warnings 返回全部 WARNING 级诊断。
func (r Report) Warnings() []RuleDiagnostic {
	var out []RuleDiagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityWarning {
			out = append(out, d)
		}
	}
	return out
}

// blockIPs 是 hosts 语义中的"黑洞地址"：命中即拦截，不产生重写。
var blockIPs = map[string]bool{
	"0.0.0.0":   true,
	"127.0.0.1": true,
	"::":        true,
	"::1":       true,
}

// Parse 解析整份规则文本。
func Parse(text, sourceID string, defaultKind RuleKind) Report {
	var (
		network  []CompiledNetworkRule
		cosmetic []CosmeticRule
		diags    []RuleDiagnostic
		ignored  int
	)
	lineNo := 0
	for _, rawLine := range splitLines(text) {
		lineNo++
		out := ParseLine(rawLine, lineNo, sourceID, defaultKind)
		switch out.Kind {
		case OutcomeNetwork:
			network = append(network, *out.Rule)
			diags = append(diags, out.Warnings...)
		case OutcomeCosmetic:
			cosmetic = append(cosmetic, *out.Cosmetic)
		case OutcomeIgnored:
			ignored++
		case OutcomeInvalid:
			diags = append(diags, *out.Diagnostic)
		}
	}
	return Report{
		NetworkRules:  network,
		CosmeticRules: cosmetic,
		Diagnostics:   diags,
		TotalLines:    lineNo,
		IgnoredLines:  ignored,
	}
}

// ParseLine 解析单行。
func ParseLine(rawLine string, lineNo int, sourceID string, defaultKind RuleKind) ParseOutcome {
	line := trimSpace(rawLine)

	if line == "" {
		return ignored("空行")
	}
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return ignored("列表头声明")
	}
	if strings.HasPrefix(line, "!") || strings.HasPrefix(line, ";") {
		return ignored("注释")
	}

	// 以 # 开头：既可能是全局元素规则（##.ad），也可能是 # 注释。
	//
	// 判据是"标记必须出现在行首" —— 只靠"能否解析出标记"会把
	// `# 说明（与 ## 元素规则区分）` 这种注释误判成元素规则，
	// 因为注释正文里出现的 `##` 会被当成标记。
	// Kotlin 侧原实现有此缺陷，已在两侧一并修正（见 README 移植说明）。
	if strings.HasPrefix(line, "#") {
		if hasCosmeticMarkerAtStart(line) {
			if c := ParseCosmetic(line, lineNo, sourceID); c != nil {
				return ParseOutcome{Kind: OutcomeCosmetic, Cosmetic: c}
			}
		}
		return ignored("注释")
	}

	// hosts / dnsmasq 专有格式
	if out, ok := parseHostsOrDnsmasq(line, lineNo, sourceID); ok {
		return out
	}

	// 元素类规则：必须在 $ 修饰符切分之前，因为 #$# 自身含 $
	if c := ParseCosmetic(line, lineNo, sourceID); c != nil {
		return ParseOutcome{Kind: OutcomeCosmetic, Cosmetic: c}
	}

	return parseNetwork(line, lineNo, sourceID, defaultKind)
}

// hasCosmeticMarkerAtStart 判断行首是否紧跟一个元素类标记。
func hasCosmeticMarkerAtStart(line string) bool {
	for _, m := range cosmeticMarkers {
		if strings.HasPrefix(line, m.marker) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- 网络规则

func parseNetwork(line string, lineNo int, sourceID string, defaultKind RuleKind) ParseOutcome {
	text := line
	isException := false
	if strings.HasPrefix(text, "@@") {
		isException = true
		text = text[2:]
	}
	if text == "" {
		return invalidOutcome(lineNo, line, sourceID, "`@@` 之后没有模式")
	}

	patternText, optionTokens := SplitPatternAndModifiers(text)
	if patternText == "" {
		return invalidOutcome(lineNo, line, sourceID, "缺少匹配模式")
	}

	parsed := ParseModifiers(optionTokens)
	var warnings []RuleDiagnostic
	for _, tok := range parsed.Unknown {
		warnings = append(warnings, RuleDiagnostic{
			Severity: SeverityWarning,
			LineNo:   lineNo,
			Raw:      line,
			Message: "未识别的修饰符 `" + tok + "`，已忽略。可用资源类型：" +
				strings.Join(ResourceTypeTokens(), "/"),
			SourceID: sourceID,
		})
	}

	compiled := CompilePattern(patternText, parsed.Mods.MatchCase)
	if compiled == nil {
		return invalidOutcome(lineNo, line, sourceID, "无法编译匹配模式 `"+patternText+"`")
	}

	rule := CompiledNetworkRule{
		LineNo:      lineNo,
		Raw:         line,
		SourceID:    sourceID,
		IsException: isException,
		Pattern:     compiled.Pattern,
		DomainHint:  compiled.DomainHint,
		Mods:        parsed.Mods,
		Kind:        defaultKind,
	}
	return ParseOutcome{Kind: OutcomeNetwork, Rule: &rule, Warnings: warnings}
}

// SplitPatternAndModifiers 拆分"模式"与"$修饰符"。
//
// 正则规则（`/.../`）必须先把闭合斜杠找出来，否则模式里的 `$` 锚点
// 会被误判为修饰符分隔符。
func SplitPatternAndModifiers(text string) (string, []string) {
	if strings.HasPrefix(text, "/") {
		for i := 1; i < len(text); i++ {
			if text[i] == '/' && (i == len(text)-1 || text[i+1] == '$') {
				return text[:i+1], parseOptions(text[i+1:])
			}
		}
	}
	idx := indexByte(text, '$')
	if idx < 0 {
		return text, nil
	}
	return text[:idx], parseOptions(text[idx:])
}

func parseOptions(rest string) []string {
	if !strings.HasPrefix(rest, "$") {
		return nil
	}
	body := rest[1:]
	if body == "" {
		return nil
	}
	var out []string
	for _, part := range splitByte(body, ',') {
		part = trimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ------------------------------------------------------- hosts / dnsmasq

// parseHostsOrDnsmasq 尝试按 hosts / dnsmasq 格式解析。第二个返回值为
// false 表示"本行不是这两种格式"，而不是"解析失败"（后者会返回 Invalid）。
func parseHostsOrDnsmasq(line string, lineNo int, sourceID string) (ParseOutcome, bool) {
	if directive, domain, value, ok := matchDnsmasq(line); ok {
		if domain == "" {
			return invalidOutcome(lineNo, line, sourceID, "dnsmasq 规则缺少域名"), true
		}
		// address=/d/            → 拦截
		// address=/d/1.2.3.4     → 重写到指定地址
		// local=/d/              → 本地应答（不转发上游）
		var mods RuleModifiers
		switch {
		case directive == "local":
			v := "local"
			mods = RuleModifiers{DNSRewrite: &v, RawTokens: []string{"dnsrewrite=local"}}
		case value == "":
			mods = RuleModifiers{RawTokens: []string{}}
		default:
			v := value
			mods = RuleModifiers{DNSRewrite: &v, RawTokens: []string{"dnsrewrite=" + value}}
		}
		return ParseOutcome{
			Kind: OutcomeNetwork,
			Rule: domainRule(domain, line, lineNo, sourceID, KindHosts, mods),
		}, true
	}

	if ip, hosts, ok := matchHostsLine(line); ok {
		if len(hosts) == 0 {
			return invalidOutcome(lineNo, line, sourceID, "hosts 行缺少主机名"), true
		}
		// hosts 一行可挂多个主机名，这里按第一个生成规则，其余追加为同源规则由调用方展开。
		// 为保持"一行一规则"的可追溯性，额外主机名通过 warnings 提示而不是静默丢弃。
		var mods RuleModifiers
		if blockIPs[ip] {
			mods = RuleModifiers{}
		} else {
			v := ip
			mods = RuleModifiers{DNSRewrite: &v, RawTokens: []string{"dnsrewrite=" + ip}}
		}
		rule := domainRule(hosts[0], line, lineNo, sourceID, KindHosts, mods)
		var warnings []RuleDiagnostic
		if len(hosts) > 1 {
			warnings = append(warnings, RuleDiagnostic{
				Severity: SeverityWarning,
				LineNo:   lineNo,
				Raw:      line,
				Message: "hosts 行含 " + itoa(len(hosts)) + " 个主机名，仅 `一条规则对应一个域名` 的模型可表达首个：" +
					strings.Join(hosts[1:], ", ") + "（已忽略）",
				SourceID: sourceID,
			})
		}
		return ParseOutcome{Kind: OutcomeNetwork, Rule: rule, Warnings: warnings}, true
	}

	return ParseOutcome{}, false
}

// matchDnsmasq 等价于 Kotlin 的 `^(address|local)=/([^/]+)/?(.*)$`（忽略大小写）。
//
// 返回的 domain 已转小写并去空白；value 只去空白（保留大小写，它可能是
// 一个地址或标记）。
func matchDnsmasq(line string) (directive, domain, value string, ok bool) {
	rest := line
	switch {
	case len(rest) >= 8 && toLowerASCII(rest[:8]) == "address=":
		directive, rest = "address", rest[8:]
	case len(rest) >= 6 && toLowerASCII(rest[:6]) == "local=":
		directive, rest = "local", rest[6:]
	default:
		return "", "", "", false
	}
	// 正则要求紧随一个 `/`
	if rest == "" || rest[0] != '/' {
		return "", "", "", false
	}
	rest = rest[1:]
	// `([^/]+)`：至少一个非斜杠字符
	slash := indexByte(rest, '/')
	rawDomain := rest
	tail := ""
	if slash >= 0 {
		rawDomain = rest[:slash]
		tail = rest[slash+1:]
	}
	if rawDomain == "" {
		return "", "", "", false
	}
	return directive, trimSpace(toLowerASCII(rawDomain)), trimSpace(tail), true
}

// matchHostsLine 等价于 Kotlin 的 `^(\d{1,3}(?:\.\d{1,3}){3})\s+(.+)$`。
//
// 注意正则**不校验**每段是否 <= 255（那是 ParseIpv4 的职责），这里保持一致：
// 只校验形态是 1-3 位十进制 × 4 段。这样 `999.1.1.1 x` 也会被识别为 hosts 行，
// 与 Kotlin 侧行为一致。
func matchHostsLine(line string) (ip string, hosts []string, ok bool) {
	i := 0
	for seg := 0; seg < 4; seg++ {
		if seg > 0 {
			if i >= len(line) || line[i] != '.' {
				return "", nil, false
			}
			i++
		}
		start := i
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		n := i - start
		if n < 1 || n > 3 {
			return "", nil, false
		}
	}
	ip = line[:i]

	// `\s+`
	if i >= len(line) || !isASCIISpace(line[i]) {
		return "", nil, false
	}
	for i < len(line) && isASCIISpace(line[i]) {
		i++
	}
	rest := line[i:]
	if rest == "" {
		return "", nil, false
	}
	for _, h := range splitWhitespace(rest) {
		h = toLowerASCII(trimSpace(h))
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	return ip, hosts, true
}

func domainRule(domain, raw string, lineNo int, sourceID string, kind RuleKind, mods RuleModifiers) *CompiledNetworkRule {
	d := domain
	return &CompiledNetworkRule{
		LineNo:      lineNo,
		Raw:         raw,
		SourceID:    sourceID,
		IsException: false,
		Pattern:     RulePattern{Kind: PatternDomainOnly, Domain: domain},
		DomainHint:  &d,
		Mods:        mods,
		Kind:        kind,
	}
}

// ---------------------------------------------------------------- 小工具

func ignored(reason string) ParseOutcome {
	return ParseOutcome{Kind: OutcomeIgnored, IgnoredReason: reason}
}

func invalidOutcome(lineNo int, raw, sourceID, message string) ParseOutcome {
	d := RuleDiagnostic{Severity: SeverityError, LineNo: lineNo, Raw: raw, Message: message, SourceID: sourceID}
	return ParseOutcome{Kind: OutcomeInvalid, Diagnostic: &d}
}

// splitWhitespace 按连续 ASCII 空白切分并丢弃空段。
func splitWhitespace(s string) []string {
	var out []string
	start := -1
	for i := 0; i < len(s); i++ {
		if isASCIISpace(s[i]) {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// splitLines 按 CRLF / LF / CR 切分，并去掉**恰好一个**结尾空段。
//
// 与 Kotlin 侧 `RuleParser.linesOf` 采用同一约定：行数等于换行符个数
// （即 `wc -l` 的语义）。这样"规则体检"报出的行数才和编辑器一致 ——
// 一个以换行结尾的文件不该被算成比实际多一行。
//
// 之所以只去掉**一个**而不是全部：`a\n\n\n` 按 wc -l 是 3 行
// （`a` 加两个空行），全部去掉会算成 1 行。这个细节直接决定
// TotalLines 与 IgnoredLines 的数值，必须两侧一致，否则
// 规则体检统计对不上。
func splitLines(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			out = append(out, text[start:i])
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		case '\n':
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	out = append(out, text[start:])
	if n := len(out); n > 0 && out[n-1] == "" {
		out = out[:n-1]
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
