package rule

import (
	"strings"
	"unicode"
)

// ModifiersResult 是修饰符解析结果：修饰符 + 未识别 token（供诊断）。
type ModifiersResult struct {
	Mods    RuleModifiers
	Unknown []string
}

// ParseModifiers 解析 `$` 之后的修饰符 token 列表。
//
// 覆盖 6.1 列出的全部修饰符与资源类型。无法识别的 token 不会被静默丢弃，
// 而是回传给调用方产出 WARNING 级诊断 —— "规则体检"需要能指出拼错的修饰符，
// 否则用户会以为规则生效了。
func ParseModifiers(tokens []string) ModifiersResult {
	var (
		important    bool
		badFilter    bool
		matchCase    bool
		denyAllow    []string
		clientScope  []string
		ctagScope    []string
		domainScope  *DomainScope
		thirdParty   *bool
		dnsRewrite   *string
		csp          *string
		removeParam  *string
		redirect     *string
		cookie       *string
		header       *string
		method       *string
		permissions  *string
		types        []ResourceType
		elemHide     bool
		genericHide  bool
		specificHide bool
		genericBlock bool
		inlineScript bool
		inlineFont   bool
		empty        bool
	)
	unknown := []string{}

	for _, token := range tokens {
		eq := indexByte(token, '=')
		name := token
		var value *string
		if eq >= 0 {
			name = token[:eq]
			v := token[eq+1:]
			value = &v
		}
		name = toLowerASCII(name)

		switch name {
		case "important":
			important = true
		case "badfilter":
			badFilter = true
		case "match-case":
			matchCase = true
		case "denyallow":
			denyAllow = splitList(value)
		case "client":
			clientScope = splitList(value)
		case "ctag":
			ctagScope = splitList(value)
		case "domain":
			domainScope = parseDomainScope(value)
		case "third-party", "3p":
			b := true
			thirdParty = &b
		case "first-party", "1p":
			b := false
			thirdParty = &b
		case "dnsrewrite":
			dnsRewrite = value
		case "csp":
			csp = value
		case "removeparam":
			removeParam = value
		case "redirect":
			redirect = value
		case "cookie":
			cookie = value
		case "header":
			header = value
		case "method":
			method = value
		case "permissions":
			permissions = value
		case "elemhide":
			elemHide = true
		case "generichide":
			genericHide = true
		case "specifichide":
			specificHide = true
		case "genericblock":
			genericBlock = true
		case "inline-script":
			inlineScript = true
		case "inline-font":
			inlineFont = true
		case "empty":
			empty = true
		case "all":
			types = append([]ResourceType(nil), allResourceTypes...)
		default:
			if rt, ok := ResourceTypeFromToken(name); ok && value == nil {
				types = appendResourceType(types, rt)
			} else {
				unknown = append(unknown, token)
			}
		}
	}

	return ModifiersResult{
		Mods: RuleModifiers{
			Important:     important,
			BadFilter:     badFilter,
			MatchCase:     matchCase,
			DenyAllow:     denyAllow,
			ClientScope:   clientScope,
			CtagScope:     ctagScope,
			DomainScope:   domainScope,
			ThirdParty:    thirdParty,
			DNSRewrite:    dnsRewrite,
			CSP:           csp,
			RemoveParam:   removeParam,
			Redirect:      redirect,
			Cookie:        cookie,
			Header:        header,
			Method:        method,
			Permissions:   permissions,
			ResourceTypes: types,
			ElemHide:      elemHide,
			GenericHide:   genericHide,
			SpecificHide:  specificHide,
			GenericBlock:  genericBlock,
			InlineScript:  inlineScript,
			InlineFont:    inlineFont,
			Empty:         empty,
			RawTokens:     tokens,
		},
		Unknown: unknown,
	}
}

// parseDomainScope 解析 `$domain=`。
//
// `~` 前缀表示排除，其余为包含。uBO 与 AdGuard 均用 `|` 分隔，这里同时接受 `,`。
func parseDomainScope(value *string) *DomainScope {
	if value == nil || isBlank(*value) {
		return nil
	}
	var include, exclude []string
	for _, part := range splitList(value) {
		if strings.HasPrefix(part, "~") {
			d := part[1:]
			if d != "" {
				exclude = appendUnique(exclude, d)
			}
		} else {
			include = appendUnique(include, part)
		}
	}
	return &DomainScope{Include: include, Exclude: exclude}
}

// splitList 按 `|` 或 `,` 切分，去空白、转小写、去空段、去重（保持插入顺序）。
func splitList(value *string) []string {
	if value == nil || isBlank(*value) {
		return nil
	}
	s := *value
	var out []string
	for _, part := range splitAny(s, "|,") {
		part = toLowerASCII(trimSpace(part))
		if part != "" {
			out = appendUnique(out, part)
		}
	}
	return out
}

// splitAny 按分隔符集合切分（等价于 Kotlin 的 split('|', ',')）。
func splitAny(s, seps string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if indexByte(seps, s[i]) >= 0 {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func appendResourceType(list []ResourceType, rt ResourceType) []ResourceType {
	for _, x := range list {
		if x == rt {
			return list
		}
	}
	return append(list, rt)
}

// isBlank 等价于 Kotlin 的 isNullOrBlank()/isBlank()。
func isBlank(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
