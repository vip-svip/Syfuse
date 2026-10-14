package filter

import (
	"strings"
	"testing"
	"time"
)

const corpus = `! M10 过滤语料（P0 全语法 + 优先级交叉）
[Adblock Plus 2.0]
||ads.example.com^
@@||good.ads.example.com^
||tracker.net^$important
@@||safe.net^$important
plain.example.org
0.0.0.0 hosts.block
127.0.0.1 hosts.loop
1.2.3.4 rewrite.me
address=/dnsmasq.block/
local=/dnsmasq.local/
# 注释行
; 分号注释
||badfilter.me^$badfilter
*.any.org
||sub.parent.com^
||parent.com^
@@||parent.com^
`

func loadTest(t *testing.T) *Engine {
	t.Helper()
	e := &Engine{}
	e.Load(corpus)
	return e
}

// 基础拦截/放行/重写/未命中。
func TestFilter_Basic(t *testing.T) {
	e := loadTest(t)
	cases := []struct {
		host string
		want Decision
	}{
		{"ads.example.com", Blocked},
		{"a.ads.example.com", Blocked},      // 含子域
		{"good.ads.example.com", Allowed},   // 例外（自身命中 @@）
		{"x.good.ads.example.com", Allowed}, // 例外继承子域
		{"ads.example.net", NoMatch},        // 后缀不同不误伤
		{"notads.example.com", NoMatch},     // 前缀粘连不误伤
		{"plain.example.org", Blocked},      // 纯域名
		{"hosts.block", Blocked},            // hosts
		{"hosts.loop", Blocked},
		{"dnsmasq.block", Blocked}, // dnsmasq
		{"unknown.host", NoMatch},
		{"a.any.org", Blocked},      // 通配首段（*.any.org） // 通配首段（||wild.*）
		{"wildcard.org", NoMatch},   // 通配规则不含裸域 // *.wildcard.org
		{"sub.parent.com", Allowed}, // 子域 block + 父域 @@ → 链聚合 allow
		{"parent.com", Allowed},
	}
	for _, tc := range cases {
		if got := e.Match(tc.host).Decision; got != tc.want {
			t.Errorf("Match(%q) = %s, want %s", tc.host, got, tc.want)
		}
	}
	// 重写。
	if r := e.Match("rewrite.me"); r.Decision != Rewritten || r.Rewrite != "1.2.3.4" {
		t.Errorf("rewrite.me = %+v", r)
	}
	if r := e.Match("dnsmasq.local"); r.Decision != Rewritten || r.Rewrite != "local" {
		t.Errorf("dnsmasq.local = %+v", r)
	}
	// 大小写与空白。
	if got := e.Match("  ADS.Example.COM ").Decision; got != Blocked {
		t.Errorf("大小写归一失败: %s", got)
	}
	// 空域名。
	if got := e.Match("").Decision; got != NoMatch {
		t.Errorf("空域名应未命中: %s", got)
	}
}

// 优先级链：重要放行 > 重要拦截 > 普通放行 > 普通拦截。
func TestFilter_Priority(t *testing.T) {
	e := loadTest(t)
	// safe.net: @@||safe.net^$important → 重要放行（同域名无拦截规则时依然成立）。
	if got := e.Match("safe.net").Decision; got != AllowedImportant {
		t.Errorf("safe.net = %s, want AllowedImportant", got)
	}
	// tracker.net: $important 拦截。
	if got := e.Match("tracker.net").Decision; got != BlockedImportant {
		t.Errorf("tracker.net = %s, want BlockedImportant", got)
	}
}

// 重要拦截 vs 普通放行交叉：important 拦截规则域名的子域上有 @@ 放行
// —— 链聚合后 impBlock 存在 → 仍是重要拦截。
func TestFilter_ImportantVsAllow(t *testing.T) {
	e := &Engine{}
	e.Load("||tracker.net^$important\n@@||ok.tracker.net^\n")
	if got := e.Match("ok.tracker.net").Decision; got != BlockedImportant {
		t.Fatalf("重要拦截压过普通放行: %s", got)
	}
	// 反向：普通拦截 + @@ important 放行 → 重要放行。
	e2 := &Engine{}
	e2.Load("||mixed.com^\n@@||mixed.com^$important\n")
	if got := e2.Match("mixed.com").Decision; got != AllowedImportant {
		t.Fatalf("重要放行压过普通拦截: %s", got)
	}
}

// $badfilter 精确关闭同形规则；带 $important 的同形不受不同形 badfilter 影响。
func TestFilter_Badfilter(t *testing.T) {
	e := &Engine{}
	e.Load("||annoy.com^\n||annoy.com^$badfilter\n")
	if got := e.Match("annoy.com").Decision; got != NoMatch {
		t.Fatalf("badfilter 后应未命中: %s", got)
	}
	// badfilter 不影响同域名的重要变体（非同形）。
	e2 := &Engine{}
	e2.Load("||annoy.com^\n||annoy.com^$important\n||annoy.com^$badfilter\n")
	if got := e2.Match("annoy.com").Decision; got != BlockedImportant {
		t.Fatalf("badfilter 只关同形: %s", got)
	}
}

// 统计口径。
func TestFilter_Stats(t *testing.T) {
	e := loadTest(t)
	s := e.Stats()
	if s.BlockRules < 4 || s.AllowRules < 2 {
		t.Fatalf("Block/Allow = %d/%d", s.BlockRules, s.AllowRules)
	}
	if s.Important != 2 {
		t.Fatalf("Important = %d", s.Important)
	}
	if s.Badfilter != 1 {
		t.Fatalf("Badfilter = %d", s.Badfilter)
	}
	if s.Rewrites != 2 { // 1.2.3.4 重写 + local
		t.Fatalf("Rewrites = %d", s.Rewrites)
	}
	if s.Errors != 0 || s.FirstError != "" {
		t.Fatalf("语料不应有错误: %d %q", s.Errors, s.FirstError)
	}
	if s.Comments < 4 {
		t.Fatalf("Comments = %d", s.Comments)
	}
}

// 坏行报错到行号且不中断整体。
func TestFilter_ParseErrors(t *testing.T) {
	e := &Engine{}
	e.Load("||good.com^\nnot a host!\n||also.good.com^\n")
	if got := e.Match("good.com").Decision; got != Blocked {
		t.Fatal("坏行不应中断整体")
	}
	s := e.Stats()
	if s.Errors != 1 || !strings.Contains(s.FirstError, "第 2 行") {
		t.Fatalf("应报第 2 行: %d %q", s.Errors, s.FirstError)
	}
}

// 热重载：Load 原子替换，旧规则立即失效。
func TestFilter_HotReload(t *testing.T) {
	e := &Engine{}
	e.Load("||a.com^")
	if e.Match("a.com").Decision != Blocked {
		t.Fatal("初始规则应生效")
	}
	e.Load("||b.com^")
	if e.Match("a.com").Decision != NoMatch {
		t.Fatal("重载后旧规则应失效")
	}
	if e.Match("b.com").Decision != Blocked {
		t.Fatal("重载后新规则应生效")
	}
}

// 空引擎 / 空文本。
func TestFilter_Empty(t *testing.T) {
	e := &Engine{}
	if got := e.Match("x.com").Decision; got != NoMatch {
		t.Fatalf("空引擎: %s", got)
	}
	e.Load("")
	if got := e.Match("x.com").Decision; got != NoMatch {
		t.Fatalf("空文本: %s", got)
	}
	if s := e.Stats(); s.Lines == 0 {
		t.Fatal("空文本 Stats.Lines 应为 1（Split 产生一空行）")
	}
}

// Decision.Block 语义。
func TestDecisionBlock(t *testing.T) {
	if !Blocked.Block() || !BlockedImportant.Block() {
		t.Fatal("拦截判定应 true")
	}
	if Allowed.Block() || AllowedImportant.Block() || NoMatch.Block() {
		t.Fatal("放行判定应 false")
	}
	if Blocked.String() != "blocked" || AllowedImportant.String() != "allowed-important" {
		t.Fatal("String 不符")
	}
}

// 大规则集编译+匹配性能（冒烟级：1 万条 < 1s 编译，匹配微秒级；
// 100 万条的正式指标需要真机跑 perf 门禁，沙盒 CI 先守住量级）。
func TestFilter_PerfSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	var b strings.Builder
	const n = 10000
	for i := 0; i < n; i++ {
		b.WriteString("||host")
		b.WriteString(itoa(i))
		b.WriteString(".example.com^\n")
	}
	e := &Engine{}
	start := time.Now()
	e.Load(b.String())
	if d := time.Since(start); d > time.Second {
		t.Fatalf("1 万条编译耗时 %v", d)
	}
	start = time.Now()
	for i := 0; i < 1000; i++ {
		if e.Match("host"+itoa(i)+".example.com").Decision != Blocked {
			t.Fatalf("host%d 应命中", i)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("1000 次匹配耗时 %v", d)
	}
	start = time.Now()
	for i := 0; i < 1000; i++ {
		e.Match("nohit" + itoa(i) + ".example.com")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("1000 次未命中耗时 %v", d)
	}
}
