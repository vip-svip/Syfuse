// Package rule 是 Syfuse 的规则语义引擎（Go 实现）。
//
// 本包是 Kotlin 侧 `core:rule` 的**同语义移植**，不是另起炉灶：
// 两侧共用同一份语料 `tests/corpus/*.txt` 与同一份期望表 `tests/expect.tsv`
// （见 corpus_test.go），因此"两边语义是否漂移"这件事有可执行判据，
// 而不是靠人工比对两段代码。
//
// 为什么 Go 侧也需要一份：
//  1. 守护进程（syfused）要在无 App 的情况下独立执行规则体检与规则测试
//     （`action.sh` 菜单、CLI、以及局域网面板）；
//  2. 数据面在内核宿主里，匹配必须发生在**离流量最近的地方**，
//     不可能每次匹配都跨 UDS 回 Kotlin 侧；
//  3. 规则集是安全敏感输入，需要能被独立审计。
//
// 语义来源与 Kotlin 侧一致：AdGuard Home 的规则语义 × uBlock Origin 的
// 匹配与优先级模型。独立重写，未复制任何第三方源码。
package rule

import "regexp"

// RuleKind 是规则在引擎中的语义类别。
type RuleKind string

const (
	KindFilter   RuleKind = "filter"
	KindRoute    RuleKind = "route"
	KindCosmetic RuleKind = "cosmetic"
	KindHosts    RuleKind = "hosts"
	KindBlock    RuleKind = "block"
)

// Decision 是匹配命中后施加的动作。
//
// 取值直接用**小写字符串**，与 `tests/expect.tsv` 的 `expect_decision` 列、
// 以及 UDS 上的 JSON 输出完全一致 —— 避免在三处各写一次映射表。
type Decision string

const (
	DecisionBlock   Decision = "block"
	DecisionAllow   Decision = "allow"
	DecisionRewrite Decision = "rewrite"
	DecisionNone    Decision = "none"
)

// Priority 是优先级层级，rank 越大越优先。
//
// 判定顺序严格为 6.1 规定的：
// `重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中（默认放行）`。
// 引擎不采用"先命中先返回"的短路策略，而是收集全部候选后按 rank 取最大者，
// 否则 `$important` 与 `@@` 的语义无法被正确表达。
type Priority int

const (
	PriorityNone           Priority = 0
	PriorityNormalBlock    Priority = 1
	PriorityNormalAllow    Priority = 2
	PriorityImportantBlock Priority = 3
	PriorityImportantAllow Priority = 4
)

// Name 返回与 expect.tsv / JSON 一致的小写名。
func (p Priority) Name() string {
	switch p {
	case PriorityImportantAllow:
		return "important-allow"
	case PriorityImportantBlock:
		return "important-block"
	case PriorityNormalAllow:
		return "normal-allow"
	case PriorityNormalBlock:
		return "normal-block"
	default:
		return "none"
	}
}

// ResourceType 是资源类型，对应 6.1 的资源类型修饰符。
type ResourceType string

const (
	RTDocument       ResourceType = "document"
	RTSubdocument    ResourceType = "subdocument"
	RTScript         ResourceType = "script"
	RTImage          ResourceType = "image"
	RTStylesheet     ResourceType = "stylesheet"
	RTXMLHTTPRequest ResourceType = "xmlhttprequest"
	RTWebsocket      ResourceType = "websocket"
	RTWebRTC         ResourceType = "webrtc"
	RTPing           ResourceType = "ping"
	RTObject         ResourceType = "object"
	RTMedia          ResourceType = "media"
	RTFont           ResourceType = "font"
	RTMP4            ResourceType = "mp4"
	RTOther          ResourceType = "other"
	RTPopup          ResourceType = "popup"
)

// allResourceTypes 是 `$all` 展开后的集合，顺序与 Kotlin 枚举声明一致。
var allResourceTypes = []ResourceType{
	RTDocument, RTSubdocument, RTScript, RTImage, RTStylesheet,
	RTXMLHTTPRequest, RTWebsocket, RTWebRTC, RTPing, RTObject,
	RTMedia, RTFont, RTMP4, RTOther, RTPopup,
}

// ResourceTypeTokens 返回全部资源类型 token，供诊断提示可选项。
//
// 与 Kotlin 侧 `ModifiersParser.resourceTypeTokens` 同源：顺序取自枚举声明，
// 不含 `xhr` 别名（别名只用于解析，不用于提示）。
func ResourceTypeTokens() []string {
	out := make([]string, 0, len(allResourceTypes))
	for _, t := range allResourceTypes {
		out = append(out, string(t))
	}
	return out
}

// ResourceTypeFromToken 解析资源类型 token；未知返回 false。
//
// 注意 `xhr` 是 `xmlhttprequest` 的别名（uBO 兼容），两侧都必须接受。
func ResourceTypeFromToken(token string) (ResourceType, bool) {
	t := toLowerASCII(token)
	if t == "xhr" {
		return RTXMLHTTPRequest, true
	}
	for _, rt := range allResourceTypes {
		if string(rt) == t {
			return rt, true
		}
	}
	return "", false
}

// DomainScope 是 `$domain=` 的作用域：include 为白名单式包含，
// exclude 为 `~` 前缀的排除。
//
// 语义与 uBO 一致：include 非空时请求的文档域名必须命中其一；
// exclude 命中即整体不适用。
type DomainScope struct {
	Include []string
	Exclude []string
}

// Allows 判断文档域名是否落在作用域内。
//
// documentHost 为 nil（如 DNS 层无来源域名）时，仅 Include 为空才可通过。
func (s *DomainScope) Allows(documentHost *string) bool {
	if s == nil {
		return true
	}
	var host string
	if documentHost != nil {
		host = toLowerASCII(*documentHost)
		if anyDomainMatch(host, s.Exclude) {
			return false
		}
	}
	if len(s.Include) == 0 {
		return true
	}
	if documentHost == nil {
		return false
	}
	return anyDomainMatch(host, s.Include)
}

// IsEmpty 表示没有任何 include/exclude，等价于"无限定"。
func (s *DomainScope) IsEmpty() bool {
	return s == nil || (len(s.Include) == 0 && len(s.Exclude) == 0)
}

// RuleModifiers 是一条规则的全部修饰符。未出现的修饰符保持零值。
type RuleModifiers struct {
	Important     bool
	BadFilter     bool
	MatchCase     bool
	DenyAllow     []string
	ClientScope   []string
	CtagScope     []string
	DomainScope   *DomainScope
	ThirdParty    *bool
	DNSRewrite    *string
	CSP           *string
	RemoveParam   *string
	Redirect      *string
	Cookie        *string
	Header        *string
	Method        *string
	Permissions   *string
	ResourceTypes []ResourceType
	ElemHide      bool
	GenericHide   bool
	SpecificHide  bool
	GenericBlock  bool
	InlineScript  bool
	InlineFont    bool
	Empty         bool
	// RawTokens 是原始 token 列表，用于规则体检展示与 $badfilter 签名。
	RawTokens []string
}

// IsCosmeticControl 表示该修饰符只影响元素隐藏，不参与网络拦截判定。
func (m *RuleModifiers) IsCosmeticControl() bool {
	return m.ElemHide || m.GenericHide || m.SpecificHide || m.GenericBlock
}

// PatternKind 区分编译后的三种匹配形态。
type PatternKind int

const (
	// PatternDomainOnly 表示 `||example.com^` 且不含路径：按域名后缀匹配。
	PatternDomainOnly PatternKind = iota
	// PatternIpv4Cidr 表示 IPv4 网段（6.4 网络层封禁 / IP-CIDR 分流）。
	PatternIpv4Cidr
	// PatternRegex 表示编译后的正则。
	PatternRegex
)

// RulePattern 是编译后的匹配模式。
//
// 只有三种形态：纯域名后缀匹配（DNS 层可直接命中，走 Trie）、
// IPv4 网段（最长前缀匹配），以及编译后的正则。正则统一对
// "完整 URL（若请求带 URL）否则裸域名"求值，与 uBO 的匹配模型一致。
type RulePattern struct {
	Kind PatternKind

	// DomainOnly 使用。
	Domain string

	// Ipv4Cidr 使用。
	Cidr   string
	Base   uint32
	Prefix int

	// Regex 使用。RegexSrc 保留源串，供 $badfilter 签名与规则体检展示。
	Regex    *regexp.Regexp
	RegexSrc string
}

// CompiledNetworkRule 是一条已编译的网络规则。
type CompiledNetworkRule struct {
	LineNo      int
	Raw         string
	SourceID    string
	IsException bool
	Pattern     RulePattern
	// DomainHint 用于 Trie 索引的域名提示；nil 表示只能线性扫描。
	DomainHint *string
	Mods       RuleModifiers
	Kind       RuleKind
}

// Priority 返回本规则在优先级模型中的层级。
func (r *CompiledNetworkRule) Priority() Priority {
	switch {
	case r.IsException && r.Mods.Important:
		return PriorityImportantAllow
	case !r.IsException && r.Mods.Important:
		return PriorityImportantBlock
	case r.IsException:
		return PriorityNormalAllow
	default:
		return PriorityNormalBlock
	}
}

// Action 返回本规则命中后的动作。
func (r *CompiledNetworkRule) Action() Decision {
	switch {
	case r.IsException:
		return DecisionAllow
	case r.Mods.DNSRewrite != nil:
		return DecisionRewrite
	case r.Mods.Redirect != nil:
		return DecisionRewrite
	case r.Mods.Empty:
		return DecisionRewrite
	default:
		return DecisionBlock
	}
}

// MatchRequest 是一次匹配请求。URL 可为 nil（纯 DNS 层查询只有 host）。
type MatchRequest struct {
	Host         string
	URL          *string
	ResourceType ResourceType
	DocumentHost *string
	Client       *string
	Ctag         *string
	Method       *string
	ThirdParty   *bool
}

// NewMatchRequest 构造一个只有 host 的请求，资源类型默认为 other。
func NewMatchRequest(host string) MatchRequest {
	return MatchRequest{Host: host, ResourceType: RTOther}
}

// MatchRequestOfURL 由完整 URL 推导出 host。
func MatchRequestOfURL(url string, rt ResourceType, documentHost *string) MatchRequest {
	u := url
	return MatchRequest{
		Host:         ExtractHost(url),
		URL:          &u,
		ResourceType: rt,
		DocumentHost: documentHost,
	}
}

// ExtractHost 从 URL 中提取 host。失败时返回空串，由上层决定是否报错。
func ExtractHost(url string) string {
	start := 0
	if i := indexOf(url, "://"); i >= 0 {
		start = i + 3
	}
	rest := url[start:]
	end := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' || rest[i] == '?' || rest[i] == '#' {
			end = i
			break
		}
	}
	authority := rest
	if end >= 0 {
		authority = rest[:end]
	}
	if at := lastIndexByte(authority, '@'); at >= 0 {
		authority = authority[at+1:]
	}
	// 处理 IPv6 字面量 [::1]:443
	if len(authority) > 0 && authority[0] == '[' {
		if close := indexByte(authority, ']'); close > 0 {
			return authority[1:close]
		}
		return authority
	}
	if colon := lastIndexByte(authority, ':'); colon > 0 {
		return authority[:colon]
	}
	return authority
}

// ChainEntry 是匹配链路中的一条候选记录，供 5.4 的"规则测试"展示完整链路。
type ChainEntry struct {
	LineNo        int
	Raw           string
	SourceID      string
	Priority      Priority
	Action        Decision
	Applied       bool
	SkippedReason *string
}

// MatchResult 是匹配结果。
type MatchResult struct {
	Decision    Decision
	Priority    Priority
	MatchedRule *CompiledNetworkRule
	Chain       []ChainEntry
}

// Blocked 表示决策为拦截。
func (m MatchResult) Blocked() bool { return m.Decision == DecisionBlock }

// Allowed 表示决策为放行。
func (m MatchResult) Allowed() bool { return m.Decision == DecisionAllow }

// noneResult 是"未命中任何规则"的结果。
func noneResult(chain []ChainEntry) MatchResult {
	if chain == nil {
		chain = []ChainEntry{}
	}
	return MatchResult{Decision: DecisionNone, Priority: PriorityNone, Chain: chain}
}

// Severity 是诊断级别。
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// RuleDiagnostic 是解析/校验诊断，带行号，供"规则体检"直接呈现。
type RuleDiagnostic struct {
	Severity Severity
	LineNo   int
	Raw      string
	Message  string
	SourceID string
}

// CosmeticControl 是文档级元素隐藏控制位。
type CosmeticControl struct {
	DisableElemHide     bool
	DisableGenericHide  bool
	DisableSpecificHide bool
	DisableGenericBlock bool
}

// ---------- 小工具：刻意不依赖 strings/unicode，保持与 Kotlin 的 ASCII 语义一致 ----------

// toLowerASCII 只折叠 ASCII 大写字母。
//
// 为什么不用 strings.ToLower：它按 Unicode 规则折叠（如 'İ'、'K' 的
// 各种形态），而 Kotlin 的 `String.lowercase()` 用的是 locale 无关的
// Unicode 折叠 —— 两者在非 ASCII 上并不完全一致。规则集里的域名/修饰符
// 按规范只能是 ASCII，这里显式限定 ASCII 反而让语义更可预测。
func toLowerASCII(s string) string {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return s
	}
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// trimSuffixDot 去掉结尾的 '.'。
func trimSuffixDot(s string) string {
	for len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// trimSpace 去掉首尾 ASCII 空白，与 Kotlin String.trim() 对 ASCII 的行为一致。
func trimSpace(s string) string {
	start := 0
	for start < len(s) && isASCIISpace(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isASCIISpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isASCIISpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}
