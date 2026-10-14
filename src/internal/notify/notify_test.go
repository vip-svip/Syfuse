package notify

import (
	"strings"
	"testing"
)

const corpus = `! 通知净化语料
广告
pkg:com.badapp
re:限时.*优惠
pkg:com.game re:充值
# 注释
`

func load(t *testing.T) (*Filter, *Stats) {
	t.Helper()
	f, st := Parse(corpus)
	if st.Errors != 0 {
		t.Fatalf("语料不应有错误: %+v", st)
	}
	return f, st
}

// 关键字命中标题/正文；大小写敏感按原文（中文场景无大小写问题）。
func TestKeyword(t *testing.T) {
	f, _ := load(t)
	cases := []struct {
		pkg, title, content string
		want                bool
	}{
		{"com.any", "【广告】限时特惠", "", true},     // 标题命中
		{"com.any", "正常标题", "点击领取广告福利", true}, // 正文命中
		{"com.any", "正常", "正常内容", false},
		{"com.badapp", "随便什么", "包名规则拦整包", true}, // pkg 无关键词 = 拦整包
		{"com.other", "随便什么", "包名不符", false},
	}
	for _, tc := range cases {
		m, got := f.ShouldFilter(tc.pkg, tc.title, tc.content)
		if got != tc.want {
			t.Errorf("ShouldFilter(%q,%q,%q) = %v, want %v (match=%v)",
				tc.pkg, tc.title, tc.content, got, tc.want, m)
		}
	}
}

// 正则规则 + pkg 组合。
func TestRegex(t *testing.T) {
	f, st := load(t)
	if st.Regexes != 2 || st.Scoped != 2 {
		t.Fatalf("统计: %+v", st)
	}
	if m, got := f.ShouldFilter("com.any", "限时优惠来袭", ""); !got || !strings.Contains(m.Rule, "re:") {
		t.Fatalf("正则命中: %v %v", m, got)
	}
	if _, got := f.ShouldFilter("com.any", "今天不搞活动", ""); got {
		t.Fatal("正则不应命中")
	}
	// 组合：包名限定的正则。
	if _, got := f.ShouldFilter("com.game", "充值中心", ""); !got {
		t.Fatal("组合规则应命中")
	}
	if _, got := f.ShouldFilter("com.other", "充值中心", ""); got {
		t.Fatal("组合规则包名不符不应命中")
	}
}

// 坏正则跳过计数；注释/空行。
func TestParseErrors(t *testing.T) {
	f, st := Parse("good\nre:([bad\n\n!c\n")
	if st.Errors != 1 {
		t.Fatalf("Errors = %d", st.Errors)
	}
	if st.Rules != 1 || st.Comments != 3 { // 两个空行+!c（Split 尾随空行）
		t.Fatalf("统计: %+v", st)
	}
	if _, got := f.ShouldFilter("", "", "good"); !got {
		t.Fatal("好规则应生效")
	}
}

// pkg: 后无关键词 = 拦整包（Rule.Keyword=="*"）。
func TestPkgOnlyRule(t *testing.T) {
	f, st := Parse("pkg:com.spam\n")
	if st.Scoped != 1 || st.Rules != 1 {
		t.Fatalf("统计: %+v", st)
	}
	if _, got := f.ShouldFilter("com.spam", "任意", "内容"); !got {
		t.Fatal("整包规则应命中")
	}
	if _, got := f.ShouldFilter("com.ok", "任意", "内容"); got {
		t.Fatal("其它包不应命中")
	}
}

// nil Filter 安全。
func TestNilFilter(t *testing.T) {
	var f *Filter
	if _, got := f.ShouldFilter("a", "b", "c"); got {
		t.Fatal("nil filter 不应命中")
	}
}

// 统计口径。
func TestStats(t *testing.T) {
	_, st := load(t)
	if st.Rules != 4 || st.Lines != 7 || st.Comments < 2 {
		t.Fatalf("统计: %+v", st)
	}
}
