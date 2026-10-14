// Package notify 实现通知净化引擎（M13，规范 6.3"通知净化"）：
// 对通知的 包名/标题/正文 做 关键字/正则 匹配，产出折叠或拦截决策。
//
// 规则语法（notify_keywords.conf，兼容 SY 原版"一行一个关键词"并超集）：
//
//	! 注释            # 注释             空行跳过
//	关键词             任意字段（标题/正文）包含即命中
//	pkg:com.app 关键词  限定包名（pkg: 后必须是行首第一个 token）
//	re:正则            正则匹配（对标题与正文分别尝试）
//	pkg:com.app re:正则  组合：限定包名的正则
//
// 决策：命中即拦截（折叠与否由调用方按命中数/设置决定——引擎只给
// 二元结论 + 命中的规则原文，证据留给日志）。
package notify

import (
	"fmt"
	"regexp"
	"strings"
)

// Rule 是一条净化规则。
type Rule struct {
	Raw     string         `json:"raw"`     // 规则原文
	Keyword string         `json:"keyword"` // 关键字（非 re: 规则）
	Pattern *regexp.Regexp `json:"-"`       // re: 规则的编译产物
	Pkg     string         `json:"pkg,omitempty"`
	IsRegex bool           `json:"is_regex"`
	Line    int            `json:"line"` // 规则源行号（编辑器定位）
}

// Filter 是编译后的净化器。Filter 无状态可并发；规则列表不可变
// （更新 = 重新 Parse 换指针）。
type Filter struct {
	rules []*Rule
}

// Stats 是规则集统计。
type Stats struct {
	Lines    int `json:"lines"`
	Comments int `json:"comments"`
	Rules    int `json:"rules"`
	Regexes  int `json:"regexes"`
	Scoped   int `json:"scoped"` // 限定包名的规则
	Errors   int `json:"errors"`
}

// Parse 解析规则文本。坏行（如非法正则）跳过并计入 Errors。
func Parse(text string) (*Filter, *Stats) {
	f := &Filter{}
	st := &Stats{}
	for i, raw := range strings.Split(text, "\n") {
		st.Lines++
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "!") ||
			strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			st.Comments++
			continue
		}
		r := &Rule{Raw: line, Line: i + 1}
		rest := line
		// pkg: 前缀。
		if strings.HasPrefix(rest, "pkg:") {
			fields := strings.SplitN(rest, " ", 2)
			r.Pkg = strings.TrimSpace(strings.TrimPrefix(fields[0], "pkg:"))
			if r.Pkg == "" {
				st.Errors++
				continue
			}
			st.Scoped++
			if len(fields) < 2 {
				// 只有包名没有关键词 = 拦该包全部通知。
				// 必须在这里终结：否则落到下面的普通分支会把
				// Keyword 覆盖成整行原文（Bug 教训）。
				r.Keyword = "*"
				st.Rules++
				f.rules = append(f.rules, r)
				continue
			}
			rest = strings.TrimSpace(fields[1])
		}
		// re: 正则。
		if strings.HasPrefix(rest, "re:") {
			pattern := strings.TrimSpace(strings.TrimPrefix(rest, "re:"))
			re, err := regexp.Compile(pattern)
			if err != nil {
				st.Errors++
				continue
			}
			r.Pattern, r.IsRegex = re, true
			st.Regexes++
		} else {
			r.Keyword = rest
			if r.Keyword == "" {
				st.Errors++
				continue
			}
		}
		st.Rules++
		f.rules = append(f.rules, r)
	}
	return f, st
}

// Match 是一次命中。
type Match struct {
	Rule string // 命中的规则原文
	Line int
}

// ShouldFilter 判断通知是否命中。pkg 可空（不限包名的规则才可能命中）；
// title/content 至少一个非空。
func (f *Filter) ShouldFilter(pkg, title, content string) (*Match, bool) {
	if f == nil {
		return nil, false
	}
	text := title + "\n" + content
	for _, r := range f.rules {
		if r.Pkg != "" && r.Pkg != pkg {
			continue
		}
		switch {
		case r.IsRegex:
			if r.Pattern.MatchString(title) || r.Pattern.MatchString(content) {
				return &Match{Rule: r.Raw, Line: r.Line}, true
			}
		case r.Keyword == "*":
			// 无关键词的包名规则 = 拦整包。
			return &Match{Rule: r.Raw, Line: r.Line}, true
		default:
			if strings.Contains(text, r.Keyword) {
				return &Match{Rule: r.Raw, Line: r.Line}, true
			}
		}
	}
	return nil, false
}

// Rules 返回全部规则（UI 列表）。
func (f *Filter) Rules() []*Rule { return f.rules }

// String 便于日志。
func (m *Match) String() string {
	return fmt.Sprintf("rule=%q (line %d)", m.Rule, m.Line)
}
