package script

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// cron 解析：合法/越界/坏段数/坏步进。
func TestParseCron(t *testing.T) {
	good := []string{"*/5 * * * *", "0 0 1 1 *", "1-5,10 * * * *", "30 8 * * 1-5", "*/2 9-17 * * 1,3,5"}
	for _, e := range good {
		if _, err := ParseCron(e); err != nil {
			t.Errorf("ParseCron(%q) 不应报错: %v", e, err)
		}
	}
	bad := []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 7", "a * * * *", "*/0 * * * *", "5-2 * * * *"}
	for _, e := range bad {
		if _, err := ParseCron(e); err == nil {
			t.Errorf("ParseCron(%q) 应报错", e)
		}
	}
}

// Matches：分钟/小时/月/日/周各维度 + dom&dow 同限时 OR 语义。
func TestCronMatches(t *testing.T) {
	// 2026-10-14 是周三（Weekday=3）。
	tm := time.Date(2026, 10, 14, 8, 35, 0, 0, time.Local)
	cases := []struct {
		expr string
		want bool
	}{
		{"35 8 * * *", true},
		{"36 8 * * *", false},
		{"*/5 8 * * *", true}, // 35 是 5 的倍数
		{"*/7 8 * * *", true}, // 35 是 7 的倍数 → 是的，35 是 7 的倍数 → 应 true
		{"* * * 10 *", true},
		{"* * * 11 *", false},
		{"* * 14 * *", true},
		{"* * 15 * *", false},
		{"* * * * 3", true},   // 周三
		{"* * * * 0", false},  // 周日
		{"* * 14 * 0", true},  // dom&dow 都受限：14日 OR 周日 → dom 命中
		{"* * 20 * 3", true},  // 20日 false，周三 true → OR
		{"* * 20 * 4", false}, // 都不命中
	}
	for _, tc := range cases {
		c, err := ParseCron(tc.expr)
		if err != nil {
			t.Fatalf("ParseCron(%q): %v", tc.expr, err)
		}
		if got := c.Matches(tm); got != tc.want {
			t.Errorf("%q.Matches = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

// Next：步进与剪枝的正确性。
func TestCronNext(t *testing.T) {
	tm := time.Date(2026, 10, 14, 8, 35, 30, 0, time.Local) // 含秒，Next 应对齐分钟
	c, _ := ParseCron("*/15 * * * *")
	next, ok := c.Next(tm)
	if !ok || !next.Equal(time.Date(2026, 10, 14, 8, 45, 0, 0, time.Local)) {
		t.Fatalf("Next = %v (%v)", next, ok)
	}
	// 跨月：1 月 1 日 0 点。
	c2, _ := ParseCron("0 0 1 * *")
	t2 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local)
	next2, _ := c2.Next(t2)
	if next2.Month() != time.February || next2.Day() != 1 || next2.Hour() != 0 {
		t.Fatalf("跨月 Next = %v", next2)
	}
	// 永不触发：2 月 30 日 → 5 年内找不到。
	c3, _ := ParseCron("0 0 30 2 *")
	if _, ok := c3.Next(t2); ok {
		t.Fatal("2月30日应永不触发")
	}
}

// Scheduler：注入时钟，到点跑 cron 脚本，同一分钟不重复。
func TestScheduler(t *testing.T) {
	s := openDisk(t)
	if _, err := s.Save("分钟任务", TypeCron, `$notify('tick','每分钟'); $done('ok');`, Match{}, "* * * * *"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var notified []string
	sch := NewScheduler(s, func(title, content string) { notified = append(notified, title) })
	base := time.Date(2026, 10, 14, 8, 0, 0, 0, time.Local)
	cur := base
	sch.SetClock(func() time.Time { return cur })

	// 8:00 触发。
	runs, err := sch.RunDue()
	if err != nil || len(runs) != 1 || runs[0].Err != "" {
		t.Fatalf("首轮: %v %+v", err, runs)
	}
	if len(notified) != 1 || notified[0] != "tick" {
		t.Fatalf("notify = %v", notified)
	}
	// 同一分钟再扫：不重复。
	if runs, _ := sch.RunDue(); len(runs) != 0 {
		t.Fatalf("同分钟不应重复: %+v", runs)
	}
	// 下一分钟：再次触发。
	cur = base.Add(time.Minute)
	if runs, _ := sch.RunDue(); len(runs) != 1 {
		t.Fatalf("下一分钟应触发: %+v", runs)
	}
	// 停用后不再触发。
	if _, err := s.SetEnabled("script", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	cur = base.Add(2 * time.Minute)
	if runs, _ := sch.RunDue(); len(runs) != 0 {
		t.Fatalf("停用后不应触发: %+v", runs)
	}
	// 最近记录与命中计数。
	if len(sch.Recent()) != 2 {
		t.Fatalf("Recent = %d", len(sch.Recent()))
	}
	e, _ := s.Get("script")
	if e.Hits != 2 {
		t.Fatalf("Hits = %d", e.Hits)
	}
}

// Scheduler：坏表达式的 cron 脚本记为 Err，不 panic。
func TestScheduler_BadExpr(t *testing.T) {
	s := openDisk(t)
	s.Save("坏表达式", TypeCron, "$done(1);", Match{}, "not-a-cron")
	sch := NewScheduler(s, nil)
	sch.SetClock(func() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.Local) })
	runs, err := sch.RunDue()
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if len(runs) != 1 || runs[0].Err == "" {
		t.Fatalf("坏表达式应记 Err: %+v", runs)
	}
}

// HookChain：改写叠加、headers 合并、reject 短路、非 http/不匹配跳过。
func TestHookChain(t *testing.T) {
	s := openDisk(t)
	// 名字必须 ASCII 且字典序可控：slug 相同的条目会互相覆盖。
	s.Save("B-rewrite", TypeHTTP, `$done({body: $response.body + '-B', headers: {'x-b':'2'}});`,
		Match{Domain: "api.x.com"}, "")
	s.Save("C-rewrite", TypeHTTP, `$done({body: $response.body + '-C', headers: {'x-c':'3'}});`,
		Match{}, "")
	s.Save("A-block", TypeHTTP, `$done({reject: true, status: 403});`,
		Match{URLRegex: "/secret"}, "")
	s.Save("D-off", TypeGeneral, `$done(1);`, Match{}, "")

	req := &Request{Method: "GET", URL: "https://api.x.com/page", Hostname: "api.x.com"}
	resp := &Response{Status: 200, Body: "content"}

	chain := NewHookChain(s)
	out, logs, err := chain.Apply(req, resp, Options{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.Rejected || !out.BodyModified || out.Body != "content-B-C" {
		t.Fatalf("链结果: %+v", out)
	}
	if out.Headers["x-b"] != "2" || out.Headers["x-c"] != "3" {
		t.Fatalf("headers: %v", out.Headers)
	}
	if len(out.Ran) != 2 || len(logs) != 0 {
		t.Fatalf("Ran = %v", out.Ran)
	}

	// secret URL：A-block 排最前且正则命中 → 立即拒绝，后续脚本不再跑。
	req2 := &Request{Method: "GET", URL: "https://api.x.com/secret", Hostname: "api.x.com"}
	out2, _, _ := chain.Apply(req2, resp, Options{})
	if !out2.Rejected || out2.Status != 403 {
		t.Fatalf("拦截: %+v", out2)
	}
	if len(out2.Ran) != 1 || out2.Ran[0] != "A-block" {
		t.Fatalf("reject 应短路后续脚本: %v", out2.Ran)
	}
}

// HookChain：单脚本执行错误记入 outcome.Err，链继续跑完其余脚本。
func TestHookChain_ErrContinues(t *testing.T) {
	s := openDisk(t)
	// 用仅存区别的 ASCII 名字保证 slug 不同、顺序可控。
	s.Save("A-broken", TypeHTTP, `throw new Error("x");`, Match{}, "")
	s.Save("B-good", TypeHTTP, `$done({body: 'ok'});`, Match{}, "")
	out, _, err := NewHookChain(s).Apply(
		&Request{Hostname: "h"}, &Response{Body: "orig"}, Options{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.Err == "" || !strings.Contains(out.Err, "A-broken") {
		t.Fatalf("Err = %q", out.Err)
	}
	if out.Body != "ok" { // 坏脚本不改写，好脚本继续
		t.Fatalf("Body = %q", out.Body)
	}
	if len(out.Ran) != 2 {
		t.Fatalf("Ran = %v", out.Ran)
	}
}

// 超时哨兵可被 errors.As 识别（引擎级契约）。
func TestTimeoutAsSentinel(t *testing.T) {
	_, _, err := Run("while(true){}", Options{Timeout: 100 * time.Millisecond})
	var te *timeoutError
	if !errors.As(err, &te) {
		t.Fatalf("应为 *timeoutError: %T", err)
	}
}
