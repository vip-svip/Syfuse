package rule

import (
	"regexp"
	"strings"
	"unicode"
)

// separator 是 uBO 的"分隔符"类。
//
// 语义：`^` 匹配任何非 `[a-zA-Z0-9_-.%]` 的字符，或字符串结尾。
const separator = `(?:[^a-zA-Z0-9_\-\.%]|$)`

// hostChars 是主机名标签字符集，用于从 `||` 之后读取域名前缀。
const hostChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_"

// CompiledPattern 是模式编译结果。
type CompiledPattern struct {
	Pattern    RulePattern
	DomainHint *string
}

// CompilePattern 把 uBO 风格的规则模式编译为 RulePattern。
//
// 支持的形态（判定顺序即代码顺序，互斥）：
//  1. IPv4 网段 `1.2.3.4` / `1.2.3.0/24`
//  2. `/正则/`                —— 显式正则
//  3. `||domain^`             —— 域名锚定；不含路径时降为 Trie 可索引的 DomainOnly
//  4. `http(s)://...` / `|...` —— 协议或起始锚定的 URL 模式
//  5. `example.com`           —— 合法主机名，按纯域名（hosts 语义）处理
//  6. 含正则元字符的模式       —— 正则（覆盖规范里 `ads?banner\d+/` 这类写法）
//  7. 其余                    —— 通配模式（`*` → `.*`，`^` → 分隔符）
//
// 编译失败返回 nil；调用方负责产出带行号的诊断。
func CompilePattern(pattern string, matchCase bool) *CompiledPattern {
	if pattern == "" {
		return nil
	}

	// 0) IPv4 网段：`1.2.3.4` 与 `1.2.3.0/24`。必须在主机名判定之前，
	//    否则点分十进制会被当作普通域名塞进 Trie，丢失网段语义。
	if ip, prefixStr, ok := matchIpv4Cidr(pattern); ok {
		base, valid := ParseIpv4(ip)
		if !valid {
			return nil
		}
		prefix := 32
		if prefixStr != "" {
			p, ok := parseIntOrNull(prefixStr)
			if !ok {
				return nil
			}
			prefix = p
		}
		if prefix < 0 || prefix > 32 {
			return nil
		}
		return &CompiledPattern{
			Pattern: RulePattern{Kind: PatternIpv4Cidr, Cidr: pattern, Base: base, Prefix: prefix},
		}
	}

	// 1) 显式正则
	if len(pattern) > 2 && pattern[0] == '/' && pattern[len(pattern)-1] == '/' {
		body := pattern[1 : len(pattern)-1]
		if re := compileRegex(body, matchCase); re != nil {
			return &CompiledPattern{Pattern: RulePattern{Kind: PatternRegex, Regex: re, RegexSrc: body}}
		}
		return nil
	}

	// 2) 域名锚定 ||
	if strings.HasPrefix(pattern, "||") {
		return compileDomainAnchored(pattern[2:], matchCase)
	}

	// 3) 起始锚定 / 协议锚定
	if strings.HasPrefix(pattern, "|") {
		body := pattern[1:]
		anchorEnd := strings.HasSuffix(body, "|")
		core := body
		if anchorEnd {
			core = body[:len(body)-1]
		}
		src := wildcardBody(core, true, anchorEnd)
		if re := compileRegex(src, matchCase); re != nil {
			return &CompiledPattern{Pattern: RulePattern{Kind: PatternRegex, Regex: re, RegexSrc: src}}
		}
		return nil
	}
	if strings.HasPrefix(pattern, "http://") || strings.HasPrefix(pattern, "https://") {
		src := wildcardBody(pattern, true, false)
		if re := compileRegex(src, matchCase); re != nil {
			return &CompiledPattern{Pattern: RulePattern{Kind: PatternRegex, Regex: re, RegexSrc: src}}
		}
		return nil
	}

	// 4) 裸主机名 → 纯域名
	if IsValidHostname(pattern) {
		d := toLowerASCII(pattern)
		return &CompiledPattern{
			Pattern:    RulePattern{Kind: PatternDomainOnly, Domain: d},
			DomainHint: &d,
		}
	}

	// 5) 含正则元字符 → 正则
	if looksLikeRegex(pattern) {
		if re := compileRegex(pattern, matchCase); re != nil {
			return &CompiledPattern{Pattern: RulePattern{Kind: PatternRegex, Regex: re, RegexSrc: pattern}}
		}
		return nil
	}

	// 6) 通配模式
	src := wildcardBody(pattern, false, false)
	if re := compileRegex(src, matchCase); re != nil {
		return &CompiledPattern{Pattern: RulePattern{Kind: PatternRegex, Regex: re, RegexSrc: src}}
	}
	return nil
}

// compileDomainAnchored 编译 `||` 之后的模式。
func compileDomainAnchored(rest string, matchCase bool) *CompiledPattern {
	domain, ok := readDomainPrefix(rest)
	if !ok {
		return nil
	}
	remainder := rest[len(domain):]
	lower := toLowerASCII(domain)

	// 纯域名形态：交给 Trie，DNS 层 O(标签数) 命中
	if remainder == "" || remainder == "^" {
		return &CompiledPattern{
			Pattern:    RulePattern{Kind: PatternDomainOnly, Domain: lower},
			DomainHint: &lower,
		}
	}

	// 带路径/通配：锚定在域名标签边界，避免 example.com.evil.com 被误命中
	var sb strings.Builder
	sb.WriteString(`^([a-z][a-z0-9+.\-]*:)?//(?:[^/?#@]*@)?(?:[^/?#]*\.)?`)
	sb.WriteString(regexp.QuoteMeta(lower))
	sb.WriteString(wildcardBody(remainder, false, false))
	src := sb.String()

	if re := compileRegex(src, matchCase); re != nil {
		return &CompiledPattern{
			Pattern:    RulePattern{Kind: PatternRegex, Regex: re, RegexSrc: src},
			DomainHint: &lower,
		}
	}
	return nil
}

// readDomainPrefix 从 `||` 之后读取域名前缀。
//
// 遇到 `/`、`^`、`*`、`?`、`|`、`$` 停止。结果末尾的点会被去掉
// （`||example.*` 这类写法）。
func readDomainPrefix(rest string) (string, bool) {
	i := 0
	for i < len(rest) && indexByte(hostChars, rest[i]) >= 0 {
		i++
	}
	raw := trimSuffixDot(rest[:i])
	if raw == "" {
		return "", false
	}
	return raw, true
}

// wildcardBody 生成通配模式的**正则体**（未编译）。
//
// 通配模式的元字符语义与 uBO 一致：
//   - `*` 匹配任意长度字符
//   - `^` 匹配"分隔符"：任何非 `[a-zA-Z0-9_-.%]` 的字符，或字符串结尾
//
// 注意 anchorEnd 生成的是**正则的结尾锚点 `$`**。
// Kotlin 侧此处写的是 `"\\$"`（即正则里的 `\$`），那在 Java 与 Go 中
// 都是"字面美元符"而不是结尾锚点 —— 属于原实现的一处真实缺陷，
// 已在两侧一并修正，并由 `TestPatternAnchorEnd` 与 Kotlin 的
// `起始锚定与结尾锚定` 用例共同守住。
func wildcardBody(body string, anchorStart, anchorEnd bool) string {
	var sb strings.Builder
	sb.Grow(len(body) + 16)
	if anchorStart {
		sb.WriteByte('^')
	}
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '*':
			sb.WriteString(".*")
		case '^':
			sb.WriteString(separator)
		default:
			sb.WriteString(regexp.QuoteMeta(string(body[i])))
		}
	}
	if anchorEnd {
		sb.WriteByte('$')
	}
	return sb.String()
}

// compileRegex 编译正则；失败返回 nil（对应 Kotlin 的 runCatching{}.getOrNull()）。
func compileRegex(body string, matchCase bool) *regexp.Regexp {
	src := body
	if !matchCase {
		src = "(?i)" + body
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil
	}
	return re
}

// IsValidHostname 判断是否是合法主机名：至少一个点、每段非空、
// 仅含字母数字与 `-`、不以 `-` 开头或结尾。
//
// 这是"纯域名清单"与"通配模式"的分界线。
func IsValidHostname(text string) bool {
	if text == "" || len(text) > 253 {
		return false
	}
	if strings.ContainsAny(text, "*^") {
		return false
	}
	labels := splitByte(text, '.')
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
				return false
			}
		}
	}
	return true
}

// looksLikeRegex 判断模式是否出现正则专属构造。
//
// 这是对规范中 `ads?banner\d+/` 这类写法的兼容路径。
func looksLikeRegex(text string) bool {
	if strings.Contains(text, `\d`) || strings.Contains(text, `\w`) ||
		strings.Contains(text, `\s`) || strings.Contains(text, `\b`) {
		return true
	}
	if strings.ContainsAny(text, "([{") {
		return true
	}
	// 以 / 结尾且含正则量词：ads?banner\d+/ 这类
	return strings.HasSuffix(text, "/") &&
		(strings.Contains(text, "?") || strings.Contains(text, "+") || strings.Contains(text, "*"))
}

// matchIpv4Cidr 等价于 Kotlin 的 `^(\d{1,3}(?:\.\d{1,3}){3})(?:/(\d{1,2}))?$`。
//
// 手写而不复用正则：该判定位于热路径入口，且必须与 Kotlin 的 matchEntire
// 语义逐字节一致 —— 手写比"再写一条可能被 RE2 与 Java 解释不同的正则"更可审计。
func matchIpv4Cidr(s string) (ip, prefix string, ok bool) {
	ipPart := s
	if slash := indexByte(s, '/'); slash >= 0 {
		ipPart = s[:slash]
		prefix = s[slash+1:]
		if len(prefix) < 1 || len(prefix) > 2 {
			return "", "", false
		}
		for i := 0; i < len(prefix); i++ {
			if prefix[i] < '0' || prefix[i] > '9' {
				return "", "", false
			}
		}
	}
	parts := splitByte(ipPart, '.')
	if len(parts) != 4 {
		return "", "", false
	}
	for _, p := range parts {
		if len(p) < 1 || len(p) > 3 {
			return "", "", false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return "", "", false
			}
		}
	}
	return ipPart, prefix, true
}
