package leak

import (
	"fmt"
	"testing"
)

// fakeProbe 全字段可编程的假探针：测试用确定性结论。
type fakeProbe struct {
	upstream    string
	upstreamErr error
	aaaa        bool
	aaaaErr     error
	stunIP      string
	stunErr     error
	quic        bool
	quicErr     error
	publicIPs   map[string]string // endpoint -> ip
	publicErrs  map[string]error
	routes      map[string]string
	sysFlags    map[string]bool
	sysErr      error
}

func (f *fakeProbe) DNSUpstream() (string, error) {
	return f.upstream, f.upstreamErr
}
func (f *fakeProbe) ResolveAAAA(string) (bool, error) { return f.aaaa, f.aaaaErr }
func (f *fakeProbe) STUNBinding(string) (string, error) {
	return f.stunIP, f.stunErr
}
func (f *fakeProbe) QUICReachable() (bool, error) { return f.quic, f.quicErr }
func (f *fakeProbe) PublicIP(ep string) (string, error) {
	if err, ok := f.publicErrs[ep]; ok {
		return "", err
	}
	return f.publicIPs[ep], nil
}
func (f *fakeProbe) RouteFor(d string) (string, error) { return f.routes[d], nil }
func (f *fakeProbe) SystemFlags() (map[string]bool, error) {
	return f.sysFlags, f.sysErr
}

func newChecker(t *testing.T, p Probe) *Checker {
	t.Helper()
	c, err := NewChecker(p)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return c
}

func findDim(results []RunResult, dim string) []RunResult {
	var out []RunResult
	for _, r := range results {
		if r.Dimension == dim {
			out = append(out, r)
		}
	}
	return out
}

// 全关配置：什么都不该跑。
func TestLeak_EmptyConfig(t *testing.T) {
	c := newChecker(t, &fakeProbe{})
	rep := c.Run(Config{})
	if len(rep.Results) != 0 {
		t.Fatalf("空配置应零结果，得到 %d", len(rep.Results))
	}
	if rep.Overall != "safe" {
		t.Fatalf("空结果 overall = %q, want safe", rep.Overall)
	}
}

// DNS：隧道上游 pass；运营商上游 fail + 修复建议；探测报错 skip。
func TestLeak_DNS(t *testing.T) {
	cases := []struct {
		name    string
		up      string
		uerr    error
		want    Status
		wantFix bool
	}{
		{"隧道DoH", "tun-doh", nil, Pass, false},
		{"tun前缀", "tun-local", nil, Pass, false},
		{"dot", "203.0.113.1:853(dot)", nil, Pass, false},
		{"运营商", "192.168.1.1", nil, Fail, true},
		{"探测失败", "", fmt.Errorf("no resolv"), Skip, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, &fakeProbe{upstream: tc.up, upstreamErr: tc.uerr})
			rep := c.Run(Config{CheckDNS: true})
			rs := findDim(rep.Results, "dns")
			if len(rs) != 1 {
				t.Fatalf("应恰好 1 条 dns 结果，得到 %d", len(rs))
			}
			if rs[0].Status != tc.want {
				t.Fatalf("upstream=%q status=%s, want %s", tc.up, rs[0].Status, tc.want)
			}
			if tc.wantFix && rs[0].Fix == "" {
				t.Fatal("fail 维度必须带修复建议")
			}
		})
	}
}

// IPv6：有 AAAA warn，无 AAAA pass。
func TestLeak_IPv6(t *testing.T) {
	c := newChecker(t, &fakeProbe{aaaa: true})
	rep := c.Run(Config{CheckIPv6: true})
	if got := findDim(rep.Results, "ipv6")[0].Status; got != Warn {
		t.Fatalf("有 AAAA 应 warn，得到 %s", got)
	}
	c2 := newChecker(t, &fakeProbe{})
	if got := findDim(c2.Run(Config{CheckIPv6: true}).Results, "ipv6")[0].Status; got != Pass {
		t.Fatalf("无 AAAA 应 pass，得到 %s", got)
	}
}

// STUN：可达 warn（带出口 IP 证据），不可达 pass。
func TestLeak_STUN(t *testing.T) {
	c := newChecker(t, &fakeProbe{stunIP: "203.0.113.9"})
	rs := findDim(c.Run(Config{CheckSTUN: true}).Results, "stun")
	if len(rs) != len(DefaultSTUNServers) {
		t.Fatalf("默认应测 %d 个 STUN，得到 %d", len(DefaultSTUNServers), len(rs))
	}
	for _, r := range rs {
		if r.Status != Warn {
			t.Fatalf("STUN 可达应 warn: %+v", r)
		}
	}
	c2 := newChecker(t, &fakeProbe{stunErr: fmt.Errorf("blocked")})
	for _, r := range findDim(c2.Run(Config{CheckSTUN: true}).Results, "stun") {
		if r.Status != Pass {
			t.Fatalf("STUN 被阻断应 pass: %+v", r)
		}
	}
}

// QUIC：可达 warn，不可达 pass。
func TestLeak_QUIC(t *testing.T) {
	c := newChecker(t, &fakeProbe{quic: true})
	if got := findDim(c.Run(Config{CheckQUIC: true}).Results, "quic")[0].Status; got != Warn {
		t.Fatalf("QUIC 可达应 warn，得到 %s", got)
	}
}

// IP 回显：混合出口 fail；单一出口 pass；端点失败 skip 不拖垮整体。
func TestLeak_IPEcho(t *testing.T) {
	c := newChecker(t, &fakeProbe{publicIPs: map[string]string{
		"a": "198.51.100.1", "b": "198.51.100.2",
	}})
	rs := findDim(c.Run(Config{CheckIPEcho: true, IPEchoEndpoints: []string{"a", "b"}}).Results, "ip_echo")
	if len(rs) != 2 {
		t.Fatalf("应 2 条结果，得到 %d", len(rs))
	}
	// 第二条（发现不同 IP 的那条）必须是 fail。
	if rs[0].Status != Pass || rs[1].Status != Fail {
		t.Fatalf("混合出口: want [pass,fail], got [%s,%s]", rs[0].Status, rs[1].Status)
	}
	if rs[1].Fix == "" {
		t.Fatal("混合出口必须带修复建议")
	}

	// 单一出口。
	c2 := newChecker(t, &fakeProbe{publicIPs: map[string]string{
		"a": "198.51.100.1", "b": "198.51.100.1",
	}})
	for _, r := range findDim(c2.Run(Config{CheckIPEcho: true, IPEchoEndpoints: []string{"a", "b"}}).Results, "ip_echo") {
		if r.Status != Pass {
			t.Fatalf("单一出口应 pass: %+v", r)
		}
	}

	// 全部端点失败 → skip，overall safe。
	c3 := newChecker(t, &fakeProbe{publicErrs: map[string]error{
		"a": fmt.Errorf("x"), "b": fmt.Errorf("x"),
	}})
	rep := c3.Run(Config{CheckIPEcho: true, IPEchoEndpoints: []string{"a", "b"}})
	for _, r := range findDim(rep.Results, "ip_echo") {
		if r.Status != Skip {
			t.Fatalf("端点失败应 skip: %+v", r)
		}
	}
	if rep.Overall != "safe" {
		t.Fatalf("全 skip overall = %s, want safe", rep.Overall)
	}
}

// 分流泄漏：敏感域名 direct → warn + 修复建议；proxy → pass。
func TestLeak_Split(t *testing.T) {
	c := newChecker(t, &fakeProbe{routes: map[string]string{
		"bank.": "direct", "pay.": "proxy",
	}})
	rs := findDim(c.Run(Config{CheckSplit: true, SensitiveDomains: []string{"bank.", "pay."}}).Results, "split")
	if len(rs) != 2 {
		t.Fatalf("应 2 条，得到 %d", len(rs))
	}
	if rs[0].Status != Warn || rs[1].Status != Pass {
		t.Fatalf("want [warn,pass], got [%s,%s]", rs[0].Status, rs[1].Status)
	}
	if rs[0].Fix == "" {
		t.Fatal("direct 告警必须带修复建议")
	}
}

// 系统级：Private DNS 开 → warn；正常 → pass。
func TestLeak_System(t *testing.T) {
	c := newChecker(t, &fakeProbe{sysFlags: map[string]bool{
		SysPrivateDNS: true, SysTetheringOn: false, SysCaptivePortal: false,
	}})
	if got := findDim(c.Run(Config{CheckSystem: true}).Results, "system")[0].Status; got != Warn {
		t.Fatalf("Private DNS 开应 warn，得到 %s", got)
	}
	c2 := newChecker(t, &fakeProbe{sysFlags: map[string]bool{}})
	if got := findDim(c2.Run(Config{CheckSystem: true}).Results, "system")[0].Status; got != Pass {
		t.Fatalf("正常应 pass，得到 %s", got)
	}
}

// Overall 汇总：任一 fail → leak；仅 warn → suspicious；否则 safe。
func TestLeak_Overall(t *testing.T) {
	cases := []struct {
		results []RunResult
		want    string
	}{
		{nil, "safe"},
		{[]RunResult{{Status: Pass}, {Status: Skip}}, "safe"},
		{[]RunResult{{Status: Pass}, {Status: Warn}}, "suspicious"},
		{[]RunResult{{Status: Warn}, {Status: Fail}}, "leak"},
		{[]RunResult{{Status: Fail}}, "leak"},
	}
	for i, tc := range cases {
		if got := Overall(tc.results); got != tc.want {
			t.Fatalf("case %d: overall = %s, want %s", i, got, tc.want)
		}
	}
}

// 全维度开启 + 全部干净 → 全 pass/safe；带时钟注入检查时间戳。
func TestLeak_FullCleanRun(t *testing.T) {
	c := newChecker(t, &fakeProbe{
		upstream:  "tun-doh",
		publicIPs: map[string]string{"a": "198.51.100.1", "b": "198.51.100.1"},
		routes:    map[string]string{"bank.": "proxy", "pay.": "proxy"},
		sysFlags:  map[string]bool{},
	})
	rep := c.Run(Config{
		CheckDNS: true, CheckIPv6: true, CheckSTUN: true, CheckQUIC: true,
		CheckIPEcho: true, CheckSplit: true, CheckSystem: true,
	})
	if rep.Overall != "suspicious" {
		// 默认 STUN 两个服务器 fake 都返回可达 → warn，overall suspicious。
		t.Fatalf("默认探针全可达应为 suspicious，得到 %s", rep.Overall)
	}
	for _, r := range rep.Results {
		if r.CheckedAt == "" {
			t.Fatalf("维度 %s 缺时间戳", r.Dimension)
		}
		if r.Evidence == "" {
			t.Fatalf("维度 %s 缺证据", r.Dimension)
		}
	}
}

// nil 探针拒绝构造。
func TestLeak_NilProbe(t *testing.T) {
	if _, err := NewChecker(nil); err == nil {
		t.Fatal("nil probe 应报错")
	}
}

// STUN 解析器：用构造的 XOR-MAPPED-ADDRESS 响应验证逐字节解析。
func TestLeak_ParseStun(t *testing.T) {
	msgID := []byte{0x73, 0x79, 0x66, 0x75, 0x73, 0x65, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	ip := []byte{203, 0, 113, 9}
	// XOR-MAPPED-ADDRESS 属性：type=0x0020, len=8, value=[0,fam,port16,xip32]。
	attr := make([]byte, 12)
	attr[0], attr[1] = 0x00, 0x20
	attr[2], attr[3] = 0, 8
	attr[4] = 0                // 首字节恒 0
	attr[5] = 0x01             // IPv4
	attr[6], attr[7] = 0, 0x50 // 端口 80（异或前）
	for i := 0; i < 4; i++ {
		attr[8+i] = ip[i] ^ msgID[i]
	}
	resp := make([]byte, 20+12)
	resp[0], resp[1] = 0x01, 0x01
	resp[2], resp[3] = 0, 12
	copy(resp[4:8], []byte{0x21, 0x12, 0xA4, 0x42})
	copy(resp[8:20], msgID)
	copy(resp[20:], attr)
	got, err := parseXorMappedAddress(resp, msgID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "203.0.113.9" {
		t.Fatalf("解析 IP = %s, want 203.0.113.9", got)
	}
}
