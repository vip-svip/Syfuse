package module

import (
	"strings"
	"testing"
)

const sample = `#!name=测试模块
#!desc=用于解析测试
#!author=someone
#!version=1.2.3
#!update=https://example.com/m.mod

[Rule]
DOMAIN-SUFFIX,example.com,PROXY
# DOMAIN-KEYWORD,ads,REJECT

[Host]
api.example.com = 1.2.3.4

[URL Rewrite]
^https?://example\.com/old redirect-302 https://example.com/new
`

func mustParse(t *testing.T, text string) *Module {
	t.Helper()
	m, pe := Parse(text)
	if len(pe.Warnings) != 0 {
		t.Fatalf("样例不应有告警: %v", pe.Warnings)
	}
	return m
}

// 解析：元数据、分段顺序、行数、停用行标记。
func TestModule_Parse(t *testing.T) {
	m := mustParse(t, sample)
	if m.Name != "测试模块" || m.Desc != "用于解析测试" ||
		m.Author != "someone" || m.Version != "1.2.3" {
		t.Fatalf("元数据错误: %+v", m)
	}
	if m.Update != "https://example.com/m.mod" {
		t.Fatalf("update = %q", m.Update)
	}
	if len(m.Segments) != 3 {
		t.Fatalf("应 3 个分段，得到 %d: %+v", len(m.Segments), m.Segments)
	}
	if m.Segments[0].Name != "Rule" || len(m.Segments[0].Lines) != 2 {
		t.Fatalf("Rule 分段错误: %+v", m.Segments[0])
	}
	if m.Segments[0].Lines[0].Disabled {
		t.Fatal("第一行应是启用")
	}
	if !m.Segments[0].Lines[1].Disabled {
		t.Fatal("# 前缀行应是停用")
	}
	if m.Segments[1].Lines[0].Raw != "api.example.com = 1.2.3.4" {
		t.Fatalf("Host 行错误: %+v", m.Segments[1].Lines[0])
	}
}

// 缺元数据 → 告警但不阻断；未知分段 → 告警；分段前内容 → 告警。
func TestModule_ParseWarnings(t *testing.T) {
	m, pe := Parse("orphan=1\n[Weird Seg]\nfoo=bar\n")
	if m.Name != "" {
		t.Fatal("无名模块 Name 应为空")
	}
	if len(m.Segments) != 1 || m.Segments[0].Name != "Weird Seg" {
		t.Fatalf("未知分段应保留: %+v", m.Segments)
	}
	if len(pe.Warnings) < 3 {
		t.Fatalf("应至少 3 条告警（缺 name/desc + 未知分段 + 分段前内容），得到 %v", pe.Warnings)
	}
}

// 往返：Parse → Serialize → Parse 结构一致，停用行保留。
func TestModule_RoundTrip(t *testing.T) {
	m := mustParse(t, sample)
	again, pe := Parse(m.Serialize())
	if len(pe.Warnings) != 0 {
		t.Fatalf("往返后不应有告警: %v", pe.Warnings)
	}
	if again.Name != m.Name || again.Author != m.Author {
		t.Fatalf("往返元数据丢失: %+v", again)
	}
	if len(again.Segments) != len(m.Segments) {
		t.Fatalf("往返分段数变化: %d vs %d", len(again.Segments), len(m.Segments))
	}
	for i := range m.Segments {
		if again.Segments[i].Name != m.Segments[i].Name {
			t.Fatalf("分段 %d 名称变化", i)
		}
		if len(again.Segments[i].Lines) != len(m.Segments[i].Lines) {
			t.Fatalf("分段 %d 行数变化", i)
		}
		for j := range m.Segments[i].Lines {
			if again.Segments[i].Lines[j] != m.Segments[i].Lines[j] {
				t.Fatalf("行 (%d,%d) 变化: %+v vs %+v",
					i, j, m.Segments[i].Lines[j], again.Segments[i].Lines[j])
			}
		}
	}
}

// 行级开关：停用再启用，序列化恢复原样。
func TestModule_ToggleLine(t *testing.T) {
	m := mustParse(t, sample)
	if err := m.ToggleLine("Rule", 0, false); err != nil {
		t.Fatalf("ToggleLine: %v", err)
	}
	out := m.Serialize()
	if !strings.Contains(out, "# DOMAIN-SUFFIX,example.com,PROXY") {
		t.Fatalf("停用行应带 # 前缀:\n%s", out)
	}
	if err := m.ToggleLine("Rule", 0, true); err != nil {
		t.Fatalf("ToggleLine: %v", err)
	}
	if strings.Contains(m.Serialize(), "# DOMAIN-SUFFIX") {
		t.Fatal("重新启用后不应再有 # 前缀")
	}
	// 越界与未知分段。
	if err := m.ToggleLine("Rule", 99, true); err == nil {
		t.Fatal("越界应报错")
	}
	if err := m.ToggleLine("Nope", 0, true); err == nil {
		t.Fatal("未知分段应报错")
	}
}

// 分段级开关：整段停用 = 所有行停用；分段头保留在序列化里。
func TestModule_ToggleSegment(t *testing.T) {
	m := mustParse(t, sample)
	if err := m.ToggleSegment("Host", false); err != nil {
		t.Fatalf("ToggleSegment: %v", err)
	}
	for _, l := range m.Segments[1].Lines {
		if !l.Disabled {
			t.Fatal("整段停用后所有行应停用")
		}
	}
	out := m.Serialize()
	if !strings.Contains(out, "\n[Host]\n") {
		t.Fatal("分段头必须保留")
	}
	m.ToggleSegment("Host", true)
	for _, l := range m.Segments[1].Lines {
		if l.Disabled {
			t.Fatal("重新启用后不应有停用行")
		}
	}
}

// 合并：两个模块按序叠加；停用行不进入结果。
func TestModule_Merge(t *testing.T) {
	a := mustParse(t, sample)
	b := mustParse(t, `#!name=模块B
#!desc=b

[Rule]
DOMAIN-SUFFIX,other.net,DIRECT

[MITM]
hostname = b.example.com
`)
	res, err := Merge([]*Module{a, b})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("无同键不应有冲突: %+v", res.Conflicts)
	}
	for _, want := range []string{
		"[Rule]", "DOMAIN-SUFFIX,example.com,PROXY",
		"DOMAIN-SUFFIX,other.net,DIRECT", "[Host]",
		"[URL Rewrite]", "[MITM]", "hostname = b.example.com",
	} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("合并结果缺 %q:\n%s", want, res.Text)
		}
	}
	// 停用行不进入。
	if strings.Contains(res.Text, "DOMAIN-KEYWORD") {
		t.Fatal("停用行不应进入合并结果")
	}
}

// 冲突检测：同段同键，后者覆盖前者，冲突记录双方。
func TestModule_MergeConflict(t *testing.T) {
	a := mustParse(t, "#!name=A\n#!desc=a\n[Host]\napi.x.com = 1.1.1.1\n")
	b := mustParse(t, "#!name=B\n#!desc=b\n[Host]\napi.x.com = 2.2.2.2\n")
	res, err := Merge([]*Module{a, b})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("应 1 条冲突，得到 %d: %+v", len(res.Conflicts), res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.Segment != "Host" || c.Key != "api.x.com" ||
		c.Winner != "B" || c.Loser != "A" || c.RawValue != "api.x.com = 2.2.2.2" {
		t.Fatalf("冲突字段错误: %+v", c)
	}
	// 覆盖后文本里只应有后者的值。
	if !strings.Contains(res.Text, "2.2.2.2") || strings.Contains(res.Text, "1.1.1.1") {
		t.Fatalf("覆盖语义错误:\n%s", res.Text)
	}
}

// Host 行（无 =）键取首 token；三分模块两冲突也按序记录。
func TestModule_MergeConflictHostLine(t *testing.T) {
	mk := func(name, ip string) *Module {
		return mustParse(t, "#!name="+name+"\n#!desc=d\n[Host]\ncdn.x.com "+ip+"\n")
	}
	res, err := Merge([]*Module{mk("A", "1.1.1.1"), mk("B", "2.2.2.2"), mk("C", "3.3.3.3")})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(res.Conflicts) != 2 {
		t.Fatalf("应 2 条冲突，得到 %d", len(res.Conflicts))
	}
	if res.Conflicts[0].Loser != "A" || res.Conflicts[0].Winner != "B" ||
		res.Conflicts[1].Loser != "B" || res.Conflicts[1].Winner != "C" {
		t.Fatalf("覆盖链错误: %+v", res.Conflicts)
	}
}

// nil 模块与空列表。
func TestModule_MergeEdge(t *testing.T) {
	if _, err := Merge([]*Module{nil}); err == nil {
		t.Fatal("nil 模块应报错")
	}
	res, err := Merge(nil)
	if err != nil {
		t.Fatalf("空列表: %v", err)
	}
	if res.Text != "" || len(res.Conflicts) != 0 {
		t.Fatalf("空列表应得空结果: %+v", res)
	}
}

// lineKey 单元：key=value 取 key；无 = 取首 token；空行空键。
func TestModule_LineKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a=b", "a"},
		{"  api.x.com = 1.2.3.4", "api.x.com"},
		{"cdn.x.com 1.2.3.4", "cdn.x.com"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := lineKey(c.in); got != c.want {
			t.Fatalf("lineKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
