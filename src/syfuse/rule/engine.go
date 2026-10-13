package rule

import (
	"sort"
	"strings"
)

// FilterEngine 是过滤规则匹配引擎。
//
// 核心语义（6.1）：
//
//	重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中（默认放行）
//
// 引擎**不做先命中即返回**的短路。它先收集全部候选规则，再按 Priority 的
// rank 取最大者。原因：短路会破坏 `$important` 与 `@@` 的表达能力 ——
// 例如一条普通拦截规则排在前面时，后面的重要放行规则将永远没有机会被考虑。
//
// 索引结构：
//   - 域名类规则 → DomainTrie，按标签深度查找，命中成本与规则总数无关；
//   - 网段类规则 → Ipv4CidrTrie，最长前缀匹配；
//   - 其余（含通配、正则、带路径的域名锚定）→ 线性扫描列表。
//
// 复杂度诚实说明：线性扫描列表的规模取决于规则集的写法。纯域名规则集可全部
// 进 Trie，此时单次匹配为 O(标签数)。含大量正则的规则集则退化为线性，
// 这是 uBO 系引擎的固有特性，不是本实现的缺陷。
type FilterEngine struct {
	allRules    []CompiledNetworkRule
	domainIndex *DomainTrie[CompiledNetworkRule]
	cidrIndex   *Ipv4CidrTrie[*cidrBucket]
	linearRules []CompiledNetworkRule
	// Diagnostics 是编译期诊断：$badfilter 生效记录、规则冲突等。
	Diagnostics []RuleDiagnostic
}

type cidrBucket struct {
	rules []CompiledNetworkRule
}

// RuleCount 返回生效规则总数。
func (e *FilterEngine) RuleCount() int { return len(e.allRules) }

// LinearRuleCount 返回线性扫描列表的规模，用于评估规则集写法对性能的影响。
func (e *FilterEngine) LinearRuleCount() int { return len(e.linearRules) }

// IndexedRuleCount 返回索引化规则数（域名 Trie + 网段 Trie）。
func (e *FilterEngine) IndexedRuleCount() int { return len(e.allRules) - len(e.linearRules) }

// Match 匹配一次请求。
//
// collectChain 为 true 时返回完整匹配链路（供 5.4"规则测试"展示）；
// 热路径应保持 false，避免为每次匹配分配链路对象。
func (e *FilterEngine) Match(request MatchRequest, collectChain bool) MatchResult {
	candidates := e.collectCandidates(request)
	var best *CompiledNetworkRule
	bestRank := -1
	var chain []ChainEntry
	if collectChain {
		chain = make([]ChainEntry, 0, len(candidates))
	}

	for i := range candidates {
		rule := &candidates[i]
		if skip := skipReason(rule, request); skip != "" {
			if collectChain {
				s := skip
				chain = append(chain, e.entry(rule, false, &s))
			}
			continue
		}
		hit := patternMatches(rule, request)
		if collectChain {
			var reason *string
			if !hit {
				s := "模式未命中"
				reason = &s
			}
			chain = append(chain, e.entry(rule, hit, reason))
		}
		if !hit {
			continue
		}
		rank := int(rule.Priority())
		if rank > bestRank {
			bestRank = rank
			best = rule
		}
	}

	if best == nil {
		return noneResult(chain)
	}
	return MatchResult{
		Decision:    best.Action(),
		Priority:    best.Priority(),
		MatchedRule: best,
		Chain:       chain,
	}
}

// CosmeticControlFor 返回该文档域名下的元素隐藏类控制位
// （`$elemhide` / `$generichide` / `$specifichide` / `$genericblock`）。
//
// 这些修饰符不参与网络拦截判定，必须单独查询，否则会错误地把放行规则
// 当成拦截规则。
func (e *FilterEngine) CosmeticControlFor(documentHost string, request *MatchRequest) CosmeticControl {
	probe := NewMatchRequest(documentHost)
	probe.DocumentHost = &documentHost
	if request != nil {
		probe = *request
	}
	var result CosmeticControl
	candidates := e.collectCandidates(probe)
	for i := range candidates {
		rule := &candidates[i]
		if !rule.IsException || !rule.Mods.IsCosmeticControl() {
			continue
		}
		if skipReason(rule, probe) != "" {
			continue
		}
		if !patternMatches(rule, probe) {
			continue
		}
		if rule.Mods.ElemHide {
			result.DisableElemHide = true
		}
		if rule.Mods.GenericHide {
			result.DisableGenericHide = true
		}
		if rule.Mods.SpecificHide {
			result.DisableSpecificHide = true
		}
		if rule.Mods.GenericBlock {
			result.DisableGenericBlock = true
		}
	}
	return result
}

// ------------------------------------------------------------------ 内部

func (e *FilterEngine) collectCandidates(request MatchRequest) []CompiledNetworkRule {
	host := request.Host
	indexed := make([]CompiledNetworkRule, 0, 8)
	indexed = append(indexed, e.domainIndex.Lookup(host)...)
	// 仅当请求主机本身是 IPv4 字面量时才查网段索引，避免把域名解析结果误当 IP
	if _, ok := ParseIpv4(host); ok {
		if bucket, found := e.cidrIndex.Lookup(host); found {
			indexed = append(indexed, bucket.rules...)
		}
	}
	if len(e.linearRules) == 0 {
		return indexed
	}
	out := make([]CompiledNetworkRule, 0, len(indexed)+len(e.linearRules))
	out = append(out, indexed...)
	out = append(out, e.linearRules...)
	return out
}

// skipReason 返回空串表示该规则适用于本请求；否则返回跳过原因（用于链路展示）。
func skipReason(rule *CompiledNetworkRule, req MatchRequest) string {
	mods := &rule.Mods

	// 元素隐藏控制类修饰符不参与网络拦截
	if mods.IsCosmeticControl() {
		return "元素隐藏控制类修饰符，不参与网络拦截"
	}

	if len(mods.ResourceTypes) > 0 && !containsResourceType(mods.ResourceTypes, req.ResourceType) {
		toks := make([]string, 0, len(mods.ResourceTypes))
		for _, t := range mods.ResourceTypes {
			toks = append(toks, string(t))
		}
		return "资源类型不匹配（要求 " + strings.Join(toks, "/") + "）"
	}
	if mods.ThirdParty != nil && req.ThirdParty != nil && *mods.ThirdParty != *req.ThirdParty {
		if *mods.ThirdParty {
			return "规则仅适用于第三方请求"
		}
		return "规则仅适用于第一方请求"
	}
	if len(mods.ClientScope) > 0 {
		if req.Client == nil || !containsString(mods.ClientScope, toLowerASCII(*req.Client)) {
			return "客户端标识不匹配（本机未在 " + strings.Join(mods.ClientScope, "|") + " 中）"
		}
	}
	if len(mods.CtagScope) > 0 {
		if req.Ctag == nil || !containsString(mods.CtagScope, toLowerASCII(*req.Ctag)) {
			return "标签组不匹配（本机未在 " + strings.Join(mods.CtagScope, "|") + " 中）"
		}
	}
	if mods.DomainScope != nil && !mods.DomainScope.Allows(req.DocumentHost) {
		return "来源域名不在 $domain 作用域内"
	}
	if mods.Method != nil && (req.Method == nil || !strings.EqualFold(*mods.Method, *req.Method)) {
		return "请求方法不匹配（要求 " + *mods.Method + "）"
	}
	// $denyallow：规则对列出的域名不生效
	if len(mods.DenyAllow) > 0 && anyDomainMatch(req.Host, mods.DenyAllow) {
		return "$denyallow 将该域名排除在本规则之外"
	}
	return ""
}

func patternMatches(rule *CompiledNetworkRule, req MatchRequest) bool {
	switch rule.Pattern.Kind {
	case PatternDomainOnly:
		return HostMatchesDomain(req.Host, rule.Pattern.Domain)
	case PatternIpv4Cidr:
		addr, ok := ParseIpv4(req.Host)
		if !ok {
			return false
		}
		var mask uint32
		if rule.Pattern.Prefix != 0 {
			mask = ^uint32(0) << uint(32-rule.Pattern.Prefix)
		}
		return (addr & mask) == (rule.Pattern.Base & mask)
	case PatternRegex:
		target := req.Host
		if req.URL != nil {
			target = *req.URL
		}
		return rule.Pattern.Regex.MatchString(target)
	}
	return false
}

func (e *FilterEngine) entry(rule *CompiledNetworkRule, applied bool, reason *string) ChainEntry {
	return ChainEntry{
		LineNo:        rule.LineNo,
		Raw:           rule.Raw,
		SourceID:      rule.SourceID,
		Priority:      rule.Priority(),
		Action:        rule.Action(),
		Applied:       applied,
		SkippedReason: reason,
	}
}

func containsResourceType(list []ResourceType, rt ResourceType) bool {
	for _, x := range list {
		if x == rt {
			return true
		}
	}
	return false
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Compile 编译规则集。
//
// `$badfilter` 的处理放在编译期而不是匹配期：它改变的是**规则集的构成**，
// 在匹配期处理会让"被抵消"的规则仍然参与扫描，白白浪费预算。
func Compile(rules []CompiledNetworkRule, extraDiagnostics []RuleDiagnostic) *FilterEngine {
	diagnostics := make([]RuleDiagnostic, 0, len(extraDiagnostics)+4)
	diagnostics = append(diagnostics, extraDiagnostics...)

	// 1) 收集 $badfilter 签名
	badSignatures := map[string]*CompiledNetworkRule{}
	for i := range rules {
		if rules[i].Mods.BadFilter {
			sig := SignatureOf(&rules[i])
			badSignatures[sig] = &rules[i]
		}
	}

	// 2) 剔除被抵消的规则（含 $badfilter 规则自身）
	effective := make([]CompiledNetworkRule, 0, len(rules))
	disabled := 0
	for i := range rules {
		rule := &rules[i]
		sig := SignatureOf(rule)
		if killer, ok := badSignatures[sig]; ok {
			disabled++
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: SeverityInfo,
				LineNo:   killer.LineNo,
				Raw:      killer.Raw,
				Message:  "`$badfilter` 已关闭第 " + itoa(rule.LineNo) + " 行的同形规则",
				SourceID: killer.SourceID,
			})
			continue
		}
		effective = append(effective, *rule)
	}

	// 3) 建索引
	domainIndex := NewDomainTrie[CompiledNetworkRule]()
	cidrIndex := NewIpv4CidrTrie[*cidrBucket]()
	// 同一 CIDR 可以挂多条规则，Trie 节点只能存一个值，因此用桶列表承载。
	cidrBuckets := map[string]*cidrBucket{}
	var linear []CompiledNetworkRule
	for i := range effective {
		rule := &effective[i]
		switch rule.Pattern.Kind {
		case PatternDomainOnly:
			domainIndex.Put(rule.Pattern.Domain, *rule)
		case PatternIpv4Cidr:
			bucket, ok := cidrBuckets[rule.Pattern.Cidr]
			if !ok {
				bucket = &cidrBucket{}
				cidrBuckets[rule.Pattern.Cidr] = bucket
				cidrIndex.Put(rule.Pattern.Cidr, bucket)
			}
			bucket.rules = append(bucket.rules, *rule)
		case PatternRegex:
			if rule.DomainHint != nil {
				domainIndex.Put(*rule.DomainHint, *rule)
			} else {
				linear = append(linear, *rule)
			}
		}
	}

	diagnostics = append(diagnostics, RuleDiagnostic{
		Severity: SeverityInfo,
		LineNo:   0,
		Raw:      "",
		Message: "规则编译完成：总计 " + itoa(len(rules)) + " 条，" + itoa(disabled) + " 条被 $badfilter 关闭，" +
			"索引化 " + itoa(len(effective)-len(linear)) + " 条，线性扫描 " + itoa(len(linear)) + " 条",
	})

	return &FilterEngine{
		allRules:    effective,
		domainIndex: domainIndex,
		cidrIndex:   cidrIndex,
		linearRules: linear,
		Diagnostics: diagnostics,
	}
}

// SignatureOf 计算规则签名，用于 `$badfilter` 的同形判定。
//
// 由**结构化字段**重建，而不是从原始文本裁剪，否则正则规则里的 `$` 锚点
// 会导致误判。
//
// 注意正则使用编译前的源串（RegexSrc）而不是 re.String()：Go 的
// regexp 会把 `(?i)` 折进 String()，Java 的 Regex.pattern 则不含内联旗标。
// 用源串才能让两侧签名逐字符可比。
func SignatureOf(rule *CompiledNetworkRule) string {
	var patternKey string
	switch rule.Pattern.Kind {
	case PatternDomainOnly:
		patternKey = "D:" + rule.Pattern.Domain
	case PatternIpv4Cidr:
		patternKey = "C:" + rule.Pattern.Cidr
	case PatternRegex:
		patternKey = "R:" + rule.Pattern.RegexSrc
	}
	tokens := make([]string, 0, len(rule.Mods.RawTokens))
	for _, t := range rule.Mods.RawTokens {
		if strings.EqualFold(t, "badfilter") {
			continue
		}
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	prefix := "B"
	if rule.IsException {
		prefix = "E"
	}
	return prefix + "|" + patternKey + "|" + strings.Join(tokens, ",")
}
