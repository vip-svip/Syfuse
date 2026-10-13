package rule

import "strings"

// CosmeticType 是元素/脚本类规则的子类型。
type CosmeticType string

const (
	CosmeticHide        CosmeticType = "hide"
	CosmeticUnhide      CosmeticType = "unhide"
	CosmeticCSSInject   CosmeticType = "css-inject"
	CosmeticExtendedCSS CosmeticType = "extended-css"
	CosmeticScriptlet   CosmeticType = "scriptlet"
	CosmeticHTMLFilter  CosmeticType = "html-filter"
)

// CosmeticRule 是一条元素/脚本类规则（6.1 的"元素与修饰规则"）。
//
// 应用位置说明（诚实登记，不假装全都能用）：
//   - Hide / Unhide / CSSInject / ExtendedCSS 需要渲染上下文才能生效。
//     本产品主界面是原生 Compose，不承载网页，因此这类规则**只在两个位置生效**：
//     ① MITM 解密后的 HTML 响应改写；② 可选的局域网面板（默认关闭）。
//     它们对 DNS 层与连接层拦截没有作用，引擎不会把它们当作网络拦截规则使用。
//   - HTMLFilter 作用于 MITM 响应体，属于网络层可实现的过滤，优先级最高。
//   - Scriptlet 需要脚本引擎（M7 交付）。
type CosmeticRule struct {
	LineNo   int
	Raw      string
	SourceID string
	Type     CosmeticType
	// Scope 是作用域名集合；Include 为空表示全局规则。
	Scope DomainScope
	// Body 是选择器、CSS 片段、scriptlet 调用或 HTML 过滤选择器。
	Body string
}

// IsGlobal 表示本规则无域名限定。
func (c *CosmeticRule) IsGlobal() bool { return len(c.Scope.Include) == 0 }

// AppliesTo 判断该规则是否作用于指定文档域名。
func (c *CosmeticRule) AppliesTo(documentHost *string) bool {
	return c.Scope.Allows(documentHost)
}

// cosmeticMarker 把标记与类型配成对。
type cosmeticMarker struct {
	marker string
	typ    CosmeticType
}

// cosmeticMarkers 按长度降序排列：必须先匹配 `##^`、`#@#` 等多字符标记，
// 否则会被 `##` 抢先切分。
var cosmeticMarkers = []cosmeticMarker{
	{"##^", CosmeticHTMLFilter},
	{"#@#", CosmeticUnhide},
	{"#$#", CosmeticCSSInject},
	{"#?#", CosmeticExtendedCSS},
	{"#%#", CosmeticScriptlet},
	{"##", CosmeticHide},
}

// ParseCosmetic 尝试解析为元素类规则。返回 nil 表示本行不是元素类规则。
//
// 注意调用顺序：本函数必须在 `$` 修饰符切分**之前**调用，因为 `#$#` 自身含 `$`。
func ParseCosmetic(line string, lineNo int, sourceID string) *CosmeticRule {
	for _, m := range cosmeticMarkers {
		idx := indexOf(line, m.marker)
		if idx < 0 {
			continue
		}
		domainPart := line[:idx]
		body := line[idx+len(m.marker):]
		if body == "" {
			return nil
		}
		return &CosmeticRule{
			LineNo:   lineNo,
			Raw:      line,
			SourceID: sourceID,
			Type:     m.typ,
			Scope:    parseCosmeticScope(domainPart),
			Body:     body,
		}
	}
	return nil
}

// parseCosmeticScope 解析作用域：逗号分隔，`~` 前缀为排除。空串表示全局。
func parseCosmeticScope(domainPart string) DomainScope {
	if isBlank(domainPart) {
		return DomainScope{}
	}
	var include, exclude []string
	for _, part := range strings.Split(domainPart, ",") {
		d := toLowerASCII(trimSpace(part))
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, "~") {
			stripped := d[1:]
			if stripped != "" {
				exclude = appendUnique(exclude, stripped)
			}
		} else {
			include = appendUnique(include, d)
		}
	}
	return DomainScope{Include: include, Exclude: exclude}
}
