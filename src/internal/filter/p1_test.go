package filter

import "testing"

// 正则规则：拦截、例外、@@ 优先级。
func TestFilter_Regex(t *testing.T) {
	e := &Engine{}
	e.Load("/^ads[0-9]+\\./\n@@/^ads0/\n")
	if got := e.Match("ads123.example").Decision; got != Blocked {
		t.Fatalf("正则拦截: %s", got)
	}
	if got := e.Match("ads0.example").Decision; got != Allowed {
		t.Fatalf("正则例外: %s", got)
	}
	if got := e.Match("normal.example").Decision; got != NoMatch {
		t.Fatalf("未命中: %s", got)
	}
	// 坏正则报错不中断。
	e2 := &Engine{}
	e2.Load("/good/\n/([unclosed/\n||ok.com^\n")
	if e2.Match("good.com").Decision != Blocked || e2.Match("ok.com").Decision != Blocked {
		t.Fatal("坏正则不应中断")
	}
	if s := e2.Stats(); s.Errors != 1 || s.FirstError == "" {
		t.Fatalf("坏正则应计错误: %+v", s)
	}
}

// $client=/$ctag= 作用域：无上下文跳过；上下文匹配生效；优先级链不变。
func TestFilter_Scope(t *testing.T) {
	e := &Engine{}
	e.Load("||ads.com^$client=phone1\n||tracker.com^\n@@||ads.com^$client=phone2\n")
	// 无上下文：client 作用域规则跳过，普通规则照常。
	if got := e.Match("tracker.com").Decision; got != Blocked {
		t.Fatalf("普通拦截: %s", got)
	}
	if got := e.Match("ads.com").Decision; got != NoMatch {
		t.Fatalf("无上下文作用域应跳过: %s", got)
	}
	// phone1：拦截。
	if got := e.MatchCtx("ads.com", "phone1", nil).Decision; got != Blocked {
		t.Fatalf("phone1 拦截: %s", got)
	}
	// phone2：例外。
	if got := e.MatchCtx("ads.com", "phone2", nil).Decision; got != Allowed {
		t.Fatalf("phone2 例外: %s", got)
	}
	// ctag：标签匹配。
	e2 := &Engine{}
	e2.Load("||x.com^$ctag=gaming\n")
	if got := e2.MatchCtx("x.com", "", []string{"other"}).Decision; got != NoMatch {
		t.Fatalf("ctag 不匹配应跳过: %s", got)
	}
	if got := e2.MatchCtx("x.com", "", []string{"a", "gaming"}).Decision; got != Blocked {
		t.Fatalf("ctag 匹配应拦截: %s", got)
	}
}

// DNS 层无语义修饰符：接受 + Ignored 计数，规则本体仍生效。
func TestFilter_IgnoredMods(t *testing.T) {
	e := &Engine{}
	e.Load("||x.com^$domain=evil.com\n||y.com^$3p\n")
	if got := e.Match("x.com").Decision; got != Blocked {
		t.Fatalf("规则本体应生效: %s", got)
	}
	s := e.Stats()
	if s.Ignored != 2 {
		t.Fatalf("Ignored = %d, want 2", s.Ignored)
	}
}

// 四种格式嗅探。
func TestFilter_DetectFormat(t *testing.T) {
	cases := []struct {
		text string
		want Format
	}{
		{"||ads.com^\n", FormatClassical},
		{"0.0.0.0 ads.com\n", FormatClassical},
		{"ads.com\nother.com\n", FormatDomain},
		{"1.2.3.4\n5.6.7.0/24\n", FormatIPCIDR},
		{"! comment\n10.0.0.0/8\n", FormatIPCIDR},
	}
	for _, tc := range cases {
		if got := DetectFormat(tc.text); got != tc.want {
			t.Errorf("DetectFormat(%q) = %s, want %s", tc.text, got, tc.want)
		}
	}
}

// ipcidr 加载与 MatchIP。
func TestFilter_IPCIDR(t *testing.T) {
	e := &Engine{}
	if err := e.LoadFormat("1.2.3.4\n10.0.0.0/8\n192.168.1.0/24\n", FormatIPCIDR); err != nil {
		t.Fatalf("LoadFormat: %v", err)
	}
	cases := []struct {
		ip   string
		want bool
	}{
		{"1.2.3.4", true},
		{"10.1.2.3", true},  // /8 内
		{"11.0.0.1", false}, // /8 外
		{"192.168.1.99", true},
		{"192.168.2.99", false},
		{"8.8.8.8", false},
		{"not-an-ip", false},
	}
	for _, tc := range cases {
		if got := e.MatchIP(tc.ip); got != tc.want {
			t.Errorf("MatchIP(%q) = %v, want %v", tc.ip, got, tc.want)
		}
	}
	// 坏行计数。
	e2 := &Engine{}
	e2.LoadIPCIDR("1.2.3.4\nbad line\n300.1.1.1\n")
	if s := e2.Stats(); s.Errors != 2 {
		t.Fatalf("坏行应计 2 错: %+v", s)
	}
}

// MRS 明确拒绝（P2 路线）。
func TestFilter_MRS(t *testing.T) {
	e := &Engine{}
	if err := e.LoadFormat("binary", FormatMRS); err == nil {
		t.Fatal("MRS 应明确报错")
	}
	if err := e.LoadFormat("x", Format("unknown")); err == nil {
		t.Fatal("未知格式应报错")
	}
}

// domain 格式加载（纯域名行原生支持）。
func TestFilter_DomainFormat(t *testing.T) {
	e := &Engine{}
	if err := e.LoadFormat("a.com\nb.com\n", FormatDomain); err != nil {
		t.Fatalf("LoadFormat: %v", err)
	}
	if e.Match("a.com").Decision != Blocked || e.Match("b.com").Decision != Blocked {
		t.Fatal("domain 格式应全部拦截")
	}
}
