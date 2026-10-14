// Package filter 实现 M10 过滤引擎（规范 5.12）的 P0 层：
// AGH/uBO/SY-Fusion 语法的 DNS 层拦截 —— 解析（报错到行号）→ 编译
// （域名 Trie + 精确域名表 + hosts 重写表）→ 优先级匹配 → 热重载。
//
// 优先级链（必须严格实现，黄金用例守住）：
//
//	重要放行($important 的 @@) > 重要拦截($important) >
//	普通放行(@@) > 普通拦截 > 未命中(默认放行)
//
// $badfilter 精确关闭同形规则（去掉 $badfilter 修饰后逐字节相同的行）。
// 语义对齐 SY-Fusion engine.sh 的判定顺序（规范 6.5"Go 重写 + 语料回归"）。
package filter

import (
	"fmt"
	"strings"
	"sync"
)

// Decision 是一次匹配的结论。
type Decision int

const (
	// NoMatch 未命中任何规则 —— 默认放行。
	NoMatch Decision = iota
	// Blocked 普通拦截。
	Blocked
	// Allowed 普通放行（@@ 命中）。
	Allowed
	// BlockedImportant 重要拦截（$important）。
	BlockedImportant
	// AllowedImportant 重要放行（@@ + $important）。
	AllowedImportant
	// Rewritten hosts/dnsmasq 解析重写命中（值在 MatchResult 里）。
	Rewritten
)

// String 便于日志与统计。
func (d Decision) String() string {
	switch d {
	case NoMatch:
		return "nomatch"
	case Blocked:
		return "blocked"
	case Allowed:
		return "allowed"
	case BlockedImportant:
		return "blocked-important"
	case AllowedImportant:
		return "allowed-important"
	case Rewritten:
		return "rewritten"
	}
	return fmt.Sprintf("Decision(%d)", int(d))
}

// Block 汇总"是否拦截"。
func (d Decision) Block() bool {
	return d == Blocked || d == BlockedImportant
}

// MatchResult 是一次 Match 的完整结果（拦截决策 + 可选重写值）。
type MatchResult struct {
	Decision Decision
	Rewrite  string // Rewritten 时的 IP / 本地值；其它情形为空
	Rule     string // 命中的规则原文（拦截日志"按规则筛选"）
}

// Engine 是编译后的过滤引擎。读侧无锁（编译完整体换指针），
// 写侧 Load 持锁 —— 热重载不中断匹配。
type Engine struct {
	mu      sync.RWMutex
	current *compiled
}

// compiled 是一次编译的全部产物。
type compiled struct {
	// domainTrie 承载 ||锚定 与 纯域名（含子域匹配）。
	domainTrie *trieNode
	// exact 是 hosts/dnsmasq 的精确域名表：域名 -> 规则原文。
	// hosts 重写（1.2.3.4 x.com）与 local=/x/ 在 rewrites 里。
	exact map[string]struct{}
	// rewrites 是 hosts IP 重写与 dnsmasq local：域名 -> 值。
	rewrites map[string]string
	// badfilter 是被 $badfilter 关闭的规则集合（去修饰后的原文）。
	badfilter map[string]bool
	// p1 是 P1 层（正则/作用域/ipcidr），见 p1.go。
	p1 *p1State
	// stats 是编译概况。
	Stats Stats
}

// Stats 是规则集统计（UDS filter.stats 的数据源）。
type Stats struct {
	Lines      int    `json:"lines"`       // 总行数（含注释）
	Comments   int    `json:"comments"`    // 注释/空行
	BlockRules int    `json:"block_rules"` // 拦截规则（含 hosts/dnsmasq）
	AllowRules int    `json:"allow_rules"` // 例外规则
	Important  int    `json:"important"`   // 带 $important 的规则
	Badfilter  int    `json:"badfilter"`   // $badfilter 条数
	Rewrites   int    `json:"rewrites"`    // hosts IP 重写 / dnsmasq local
	Errors     int    `json:"errors"`      // 解析失败的行数
	FirstError string `json:"first_error,omitempty"`
	Ignored    int    `json:"ignored"` // DNS 层无语义但合法接受的修饰符规则
}

// ParseError 是单行解析失败的信息（报错到行号）。
type ParseError struct {
	Line int
	Text string
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("filter: 第 %d 行 %q: %v", e.Line, e.Text, e.Err)
}

// Load 解析并编译规则文本，原子替换当前规则集（热重载）。
// 单行解析失败不中断整体（跳过并计数），但第一个错误会留在 Stats 里。
func (e *Engine) Load(text string) {
	c := compile(text)
	e.mu.Lock()
	e.current = c
	e.mu.Unlock()
}

// Stats 返回当前规则集统计；引擎为空时返回零值。
func (e *Engine) Stats() Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.current == nil {
		return Stats{}
	}
	return e.current.Stats
}

// Match 对域名做过滤决策（无客户端上下文；作用域规则被跳过）。
func (e *Engine) Match(host string) MatchResult {
	return e.MatchCtx(host, "", nil)
}

// MatchCtx 带客户端上下文的过滤决策：$client=/$ctag= 作用域规则参与判定。
// client 为空、tags 为 nil 时等价于 Match。
func (e *Engine) MatchCtx(host, client string, tags []string) MatchResult {
	e.mu.RLock()
	c := e.current
	e.mu.RUnlock()
	if c == nil {
		return MatchResult{Decision: NoMatch}
	}
	var ctx *matchCtx
	if client != "" || len(tags) > 0 {
		ctx = &matchCtx{client: client, tags: tags}
	}
	return c.match(lowerTrim(host), ctx)
}

// compile 把文本编译成 compiled。
func compile(text string) *compiled {
	c := &compiled{
		domainTrie: &trieNode{},
		exact:      map[string]struct{}{},
		rewrites:   map[string]string{},
		badfilter:  map[string]bool{},
		p1:         &p1State{ipSet: map[string]bool{}},
	}
	type pendingRule struct {
		normalized string // 去掉 $badfilter 后的规范形
		kind       ruleKind
		important  bool
		allow      bool
		raw        string
		host       string
	}
	var pending []pendingRule
	badfilterCount := 0

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		c.Stats.Lines++
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			c.Stats.Comments++
			continue
		}
		if isComment(trimmed) {
			c.Stats.Comments++
			continue
		}

		// 正则行：/pattern/、/pattern/$mods、@@/pattern/…。
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "@@/") {
			if re, rr, perr := parseRegexRule(trimmed); perr == nil {
				if rr.badfilter {
					badfilterCount++
					c.badfilter[rr.normalized] = true
					continue
				}
				if rr.important {
					c.Stats.Important++
				}
				if rr.allow {
					c.Stats.AllowRules++
					c.p1.regexAllow = append(c.p1.regexAllow, re)
				} else {
					c.Stats.BlockRules++
					c.p1.regexSet = append(c.p1.regexSet, re)
				}
				continue
			}
			// 以 / 开头但不是合法正则行 → 落到普通解析（会得到行号错误）。
		}
		r, err := parseRule(trimmed)
		if err != nil {
			c.Stats.Errors++
			if c.Stats.FirstError == "" {
				c.Stats.FirstError = (&ParseError{Line: i + 1, Text: trimmed, Err: err}).Error()
			}
			continue
		}
		if r.badfilter {
			badfilterCount++
			// badfilter 关闭的是"去掉 $badfilter 修饰后的同形规则"。
			c.badfilter[r.normalized] = true
			continue
		}
		// 注意：badfilter 可能出现在同形规则**之后**，所以这里不查表，
		// 统一在循环结束后过滤（见下方 pending 过滤）。
		switch r.kind {
		case kindBlock:
			c.Stats.BlockRules++
		case kindAllow:
			c.Stats.AllowRules++
		}
		if r.important {
			c.Stats.Important++
		}
		if r.ignoreDNS {
			c.Stats.Ignored++
		}
		if r.client != "" || r.ctag != "" {
			// 作用域规则：判定走 scopeRules（需客户端上下文）。
			sr := &scopeRule{
				normalized: r.normalized,
				kind:       r.kind,
				important:  r.important,
				client:     r.client,
				ctag:       r.ctag,
				host:       r.host,
			}
			if r.allow {
				sr.kind = kindAllow
			}
			c.p1.scopeRules = append(c.p1.scopeRules, sr)
			// 作用域规则不能同时进 Trie/普通 pending：
			// 否则无上下文时会被当成无条件规则。
			continue
		}
		switch r.kind {
		case kindHostRewrite, kindLocal:
			c.Stats.Rewrites++
			c.rewrites[r.host] = r.value
		case kindHostsBlock, kindDnsmasq:
			c.exact[r.host] = struct{}{}
			if r.important {
				// hosts/dnsmasq 形态不支持 $important（语义上无法放行），
				// 记入 exact 即可；统计里已计 important。
			}
		case kindBlock, kindAllow:
			pending = append(pending, pendingRule{
				normalized: r.normalized, kind: r.kind,
				important: r.important, allow: r.allow, raw: r.raw, host: r.host,
			})
		}
	}
	_ = badfilterCount
	c.Stats.Badfilter = badfilterCount

	// badfilter 集合已齐：剔除被关闭的规则后再装 Trie。
	kept := pending[:0]
	for _, p := range pending {
		if !c.badfilter[p.normalized] {
			kept = append(kept, p)
		}
	}
	pending = kept
	// Trie 只装 ||/纯域名：先装入普通，再装 important 覆盖标志（每节点
	// 记两个标志位：block/allow + important 变体）。
	for _, p := range pending {
		c.domainTrie.insert(p.host, trieFlags{
			block: p.kind == kindBlock && !p.allow,
			allow: p.allow,
		})
		// important 标志单独存（同域名可能同时有普通/重要两种规则，
		// 优先级链按"存在更高级别就用更高级别"）。
		if p.important {
			c.domainTrie.markImportant(p.host, p.allow)
		}
	}
	return c
}

func isComment(s string) bool {
	return strings.HasPrefix(s, "!") || strings.HasPrefix(s, "#") ||
		strings.HasPrefix(s, ";") || strings.HasPrefix(s, "[Adblock Plus")
}

// ruleKind 是规则种类。
type ruleKind int

const (
	kindBlock       ruleKind = iota // ||x^ 或纯域名（拦截）
	kindAllow                       // @@ 例外
	kindHostsBlock                  // 0.0.0.0 x.com
	kindHostRewrite                 // 1.2.3.4 x.com（IP 重写）
	kindDnsmasq                     // address=/x/
	kindLocal                       // local=/x/
)

type rule struct {
	kind       ruleKind
	host       string // 主域名（小写）
	value      string // hosts 重写 IP / local 值
	important  bool
	badfilter  bool
	allow      bool
	client     string // $client= 值
	ctag       string // $ctag= 值
	ignoreDNS  bool   // 带 DNS 层无语义修饰符（$domain=/$3p/…）
	normalized string // 去掉 $badfilter 后的规范形（badfilter 匹配用）
	raw        string
}

// knownMods 是合法修饰符名集合（带值的形如 client=xxx 也按名匹配）。
var knownMods = map[string]bool{
	"important": true, "badfilter": true, "dnstype": true, "client": true,
	"ctag": true, "domain": true, "3p": true, "1p": true,
	"third-party": true, "first-party": true, "denyallow": true,
	"dnsrewrite": true, "match-case": true, "all": true, "document": true,
	"script": true, "image": true, "stylesheet": true, "xmlhttprequest": true,
	"subdocument": true, "websocket": true, "webrtc": true, "ping": true,
	"object": true, "media": true, "font": true, "other": true, "empty": true,
	"popup": true, "mp4": true, "generichide": true, "specifichide": true,
	"elemhide": true, "genericblock": true,
}

// splitMods 把行拆成主体与修饰符 map；ok=false 表示 $ 后不是修饰符段
// （主体本身含 $，如正则里的量词 —— 这种行交给原样解析）。
func splitMods(line string) (string, map[string]string, bool) {
	i := strings.LastIndex(line, "$")
	if i <= 0 {
		return line, nil, true
	}
	mods := map[string]string{}
	for _, m := range strings.Split(strings.ToLower(line[i+1:]), ",") {
		if m == "" {
			return line, nil, false
		}
		name, val := m, ""
		if j := strings.Index(m, "="); j >= 0 {
			name, val = m[:j], m[j+1:]
		}
		if !knownMods[name] {
			return line, nil, false
		}
		mods[name] = val
	}
	return line[:i], mods, true
}

// parseRule 解析单行。P0 只认锚定/纯域名/hosts/dnsmasq 四族 + 修饰符。
// 修饰符放在行尾 $ 后；`$` 前是主体。
func parseRule(line string) (*rule, error) {
	r := &rule{raw: line}
	body := line

	if b, mods, ok := splitMods(line); ok {
		body = b
		for name, val := range mods {
			switch name {
			case "important":
				r.important = true
			case "badfilter":
				r.badfilter = true
			case "client":
				r.client = val
			case "ctag":
				r.ctag = val
			default:
				// DNS 层无语义但合法：接受 + 忽略计数（规范 6.4 并集原则）。
				r.ignoreDNS = true
			}
		}
	}

	// 例外。
	if strings.HasPrefix(body, "@@") {
		r.allow = true
		body = body[2:]
	}

	// 四族主体。
	switch {
	case strings.HasPrefix(body, "||"):
		host := strings.TrimSuffix(strings.TrimPrefix(body, "||"), "^")
		host = strings.TrimSuffix(host, "*") // ||x^* 罕见写法容错
		if !validHost(host) {
			return nil, fmt.Errorf("域名 %q 非法", host)
		}
		r.kind = kindBlock
		r.host = host
		if r.allow {
			r.kind = kindAllow
		}
	case strings.HasPrefix(body, "address=/"), strings.HasPrefix(body, "local=/"):
		isLocal := strings.HasPrefix(body, "local=/")
		prefix := "address=/"
		if isLocal {
			prefix = "local=/"
		}
		host := strings.TrimSuffix(strings.TrimPrefix(body, prefix), "/")
		if !validHost(host) {
			return nil, fmt.Errorf("域名 %q 非法", host)
		}
		r.host = host
		if isLocal {
			r.kind = kindLocal
			r.value = "local"
		} else {
			r.kind = kindDnsmasq
		}
	case strings.ContainsRune(body, ' ') || strings.ContainsRune(body, '\t'):
		// hosts 形态：IP 域名
		fields := strings.Fields(body)
		if len(fields) < 2 {
			return nil, fmt.Errorf("hosts 行至少需要 IP 与域名")
		}
		if !isIP(fields[0]) {
			return nil, fmt.Errorf("%q 不是合法 IP", fields[0])
		}
		host := lowerTrim(fields[1])
		if !validHost(host) {
			return nil, fmt.Errorf("域名 %q 非法", host)
		}
		r.host = host
		if isBlackholeIP(fields[0]) {
			r.kind = kindHostsBlock
		} else {
			r.kind = kindHostRewrite
			r.value = fields[0]
		}
	default:
		// 纯域名。
		host := lowerTrim(body)
		if !validHost(host) {
			return nil, fmt.Errorf("域名 %q 非法", host)
		}
		r.kind = kindBlock
		r.host = host
	}

	r.normalized = normalizeForBadfilter(r)
	return r, nil
}

// normalizeForBadfilter 生成"同形"键：种类 + 域名 + important（badfilter
// 规则自身剥掉 $badfilter 后与目标规则同形）。
func normalizeForBadfilter(r *rule) string {
	k := r.kind
	if r.allow {
		k = kindAllow
	}
	return fmt.Sprintf("%d|%s|%v", int(k), r.host, r.important)
}

func isBlackholeIP(ip string) bool {
	switch ip {
	case "0.0.0.0", "127.0.0.1", "::", "::1":
		return true
	}
	return false
}

func isIP(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) == 4 {
		for _, p := range parts {
			if len(p) == 0 || len(p) > 3 {
				return false
			}
			for i := 0; i < len(p); i++ {
				if p[i] < '0' || p[i] > '9' {
					return false
				}
			}
			// 前导零宽松处理（hosts 文件常见 001.002 形态不拒绝）。
		}
		return true
	}
	return s == "::" || s == "::1" || strings.HasPrefix(s, "fe80:") ||
		strings.HasPrefix(s, "fd") || strings.HasPrefix(s, "2001:")
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, " \t/\\") {
		return false
	}
	// 通配符只允许 * 在首段（*.x.com），由 Trie 处理语义。
	for _, label := range strings.Split(h, ".") {
		if label == "" {
			return false
		}
	}
	return true
}

func lowerTrim(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
