// p1.go 是 M10 P1 层：正则规则、作用域修饰符（$client/$ctag）、
// MatchCtx 上下文匹配与四种规则源格式（domain/ipcidr/classical/mrs）。
//
// 设计与 P0 的关系：
//   - 正则规则进 compiled.regexSet，与 Trie 命中一起参与优先级聚合；
//   - $client=/$ctag= 作用域规则存 scopeRules，仅 MatchCtx 提供上下文时
//     参与判定（无上下文的 Match 忽略它们，与"要么生效要么忽略并记录"一致）；
//   - 其余 DNS 层无语义的修饰符（$domain=/$3p/$1p/$denyallow/$dnsrewrite）
//     解析时接受并计入 Ignored，绝不报错（规范 6.4 配置键并集原则）。
package filter

import (
	"fmt"
	"regexp"
	"strings"
)

// scopeRule 是一条带作用域的规则：主体命中后还需客户端/标签匹配。
type scopeRule struct {
	normalized string
	kind       ruleKind // block / allow
	important  bool
	client     string         // $client= 值（空 = 不限）
	ctag       string         // $ctag= 值（空 = 不限）
	host       string         // Trie 域名
	regex      *regexp.Regexp // 正则主体（host 或 URL 匹配）
}

// compiled 新增字段（P1）。
type p1State struct {
	regexSet   []*regexp.Regexp // 正则拦截
	regexAllow []*regexp.Regexp // 正则例外
	scopeRules []*scopeRule
	// ipSet 是 ipcidr 格式的精确 IP；cidrs 是网段列表。
	ipSet map[string]bool
	cidrs []cidr
	// ignored 计数：DNS 层无语义但合法接受的修饰符规则。
	ignored int
}

type cidr struct {
	ip   uint32
	bits int
}

// matchCtx 是 P1 匹配上下文。
type matchCtx struct {
	client string
	tags   []string
}

// matchP1 在 P0 精确表/Trie 之外补充正则与作用域规则，
// 返回"本层贡献的决策片段"；与 P0 结果按优先级链合并由 Engine.MatchCtx 完成。
func (c *compiled) matchP1(host string, ctx *matchCtx) (impAllow, impBlock, allow, block bool, rule string) {
	// 正则：host 整体匹配。
	for _, re := range c.p1.regexAllow {
		if re.MatchString(host) {
			allow = true
			rule = re.String()
			break
		}
	}
	for _, re := range c.p1.regexSet {
		if re.MatchString(host) {
			block = true
			if rule == "" {
				rule = re.String()
			}
			break
		}
	}
	// 作用域规则：上下文不匹配直接跳过。
	for _, sr := range c.p1.scopeRules {
		if !sr.matches(ctx) {
			continue
		}
		if sr.regex != nil {
			if !sr.regex.MatchString(host) {
				continue
			}
		} else if !hostInTrie(c, sr.host, host) {
			continue
		}
		switch {
		case sr.kind == kindAllow && sr.important:
			impAllow = true
		case sr.kind == kindAllow:
			allow = true
		case sr.important:
			impBlock = true
		default:
			block = true
		}
		if rule == "" {
			rule = sr.normalized
		}
	}
	return
}

// hostInTrie 判断规则域名是否命中 host（与 Trie 的后缀语义一致：
// 精确相等，或 host 以 ".ruleHost" 结尾）。
func hostInTrie(c *compiled, ruleHost, host string) bool {
	if ruleHost == "" || host == "" {
		return false
	}
	_ = c // 保持签名与 compiled 关联（未来正则/作用域混合判定）
	if host == ruleHost {
		return true
	}
	return len(host) > len(ruleHost)+1 &&
		strings.HasSuffix(host, ruleHost) &&
		host[len(host)-len(ruleHost)-1] == '.'
}

func (sr *scopeRule) matches(ctx *matchCtx) bool {
	if sr.client != "" && (ctx == nil || ctx.client != sr.client) {
		return false
	}
	if sr.ctag != "" {
		if ctx == nil {
			return false
		}
		ok := false
		for _, t := range ctx.tags {
			if t == sr.ctag {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// parseRegexRule 解析正则行 "/pattern/" 或 "/pattern/$mods"。
// 返回编译后的正则与规则元信息；pattern 非法时报错（行号由调用方补）。
func parseRegexRule(line string) (*regexp.Regexp, *rule, error) {
	r := &rule{raw: line}
	body := line
	if b, mods, ok := splitMods(line); ok {
		body = b
		for name := range mods {
			switch name {
			case "important":
				r.important = true
			case "badfilter":
				r.badfilter = true
			default:
				r.ignoreDNS = true
			}
		}
	}
	if strings.HasPrefix(body, "@@") {
		r.allow = true
		body = body[2:]
	}
	if len(body) < 3 || !strings.HasPrefix(body, "/") || !strings.HasSuffix(body, "/") {
		return nil, nil, fmt.Errorf("正则行缺少首尾 /")
	}
	pattern := body[1 : len(body)-1]
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("正则 %q 非法: %w", pattern, err)
	}
	r.kind = kindBlock
	if r.allow {
		r.kind = kindAllow
	}
	r.normalized = fmt.Sprintf("%d|%s", int(r.kind), pattern)
	return re, r, nil
}

// MatchIP 检查 IP 是否命中 ipcidr 规则源（网络封禁的 IP 层）。
func (e *Engine) MatchIP(ip string) bool {
	e.mu.RLock()
	c := e.current
	e.mu.RUnlock()
	if c == nil {
		return false
	}
	if c.p1.ipSet[ip] {
		return true
	}
	v := parseIPv4(ip)
	if v == 0 {
		return false
	}
	for _, cr := range c.p1.cidrs {
		if cr.bits == 0 {
			return true
		}
		mask := uint32(0xFFFFFFFF) << (32 - cr.bits)
		if v&mask == cr.ip&mask {
			return true
		}
	}
	return false
}

func parseIPv4(s string) uint32 {
	var out uint32
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return 0
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return 0
		}
		var n int
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return 0
			}
			n = n*10 + int(p[i]-'0')
		}
		if n > 255 {
			return 0
		}
		out = out<<8 | uint32(n)
	}
	return out
}

// Format 是规则源格式。
type Format string

const (
	FormatClassical Format = "classical" // AGH/uBO 标准语法（P0+P1）
	FormatDomain    Format = "domain"    // 每行一个纯域名
	FormatIPCIDR    Format = "ipcidr"    // 每行一个 IP 或 CIDR
	FormatMRS       Format = "mrs"       // AdGuard 二进制格式
)

// DetectFormat 按内容嗅探格式（规范 5.12 四种格式）。
func DetectFormat(text string) Format {
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || isComment(l) {
			continue
		}
		if strings.HasSuffix(l, ".mrs") {
			return FormatMRS
		}
		if strings.HasPrefix(l, "||") || strings.HasPrefix(l, "@@") ||
			strings.HasPrefix(l, "address=/") || strings.Contains(l, " ") {
			return FormatClassical
		}
		if strings.Contains(l, "/") { // CIDR
			return FormatIPCIDR
		}
		if isIP(l) {
			return FormatIPCIDR
		}
		// 纯域名。
		return FormatDomain
	}
	return FormatClassical
}

// LoadFormat 按格式加载规则源。
func (e *Engine) LoadFormat(text string, f Format) error {
	switch f {
	case FormatClassical:
		e.Load(text)
		return nil
	case FormatDomain:
		e.Load(text) // 纯域名行 P0 解析器原生支持
		return nil
	case FormatIPCIDR:
		e.LoadIPCIDR(text)
		return nil
	case FormatMRS:
		return fmt.Errorf("filter: MRS 二进制格式需要 AdGuard 解码器（P2 路线，见 docs/filter_gap.md）")
	}
	return fmt.Errorf("filter: 未知格式 %q", f)
}

// LoadIPCIDR 加载 ipcidr 规则源（精确 IP + CIDR 网段）。
func (e *Engine) LoadIPCIDR(text string) {
	c := compile(text) // 复用骨架（域名行会全部失败进 Errors，无碍）
	c.p1 = &p1State{ipSet: map[string]bool{}}
	for _, raw := range strings.Split(text, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" || isComment(l) {
			continue
		}
		if i := strings.Index(l, "/"); i >= 0 {
			ip := parseIPv4(l[:i])
			bits := 0
			for _, ch := range l[i+1:] {
				if ch < '0' || ch > '9' {
					bits = -1
					break
				}
				bits = bits*10 + int(ch-'0')
			}
			if ip == 0 || bits < 0 || bits > 32 {
				c.Stats.Errors++
				continue
			}
			c.p1.cidrs = append(c.p1.cidrs, cidr{ip: ip, bits: bits})
			c.Stats.BlockRules++
			continue
		}
		if isIP(l) {
			c.p1.ipSet[l] = true
			c.Stats.BlockRules++
			continue
		}
		c.Stats.Errors++
	}
	e.mu.Lock()
	e.current = c
	e.mu.Unlock()
}
