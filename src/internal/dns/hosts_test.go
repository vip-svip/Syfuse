package dns

// hosts 静态表的测试：构造/查询/本地应答构造，全部逐字段断言。

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewHostsParsesValidSpecs(t *testing.T) {
	h, err := NewHosts([]string{
		"example.com:1.2.3.4",
		"EXAMPLE.COM.:5.6.7.8",           // 大小写 + 尾点：与上一条同键
		"example.com:2001:db8::1",        // IPv6：第一个冒号分割
		"v6only.example.net:2606:2800::", // 多冒号
		"  spaced.example.org : 9.9.9.9  ",
	})
	if err != nil {
		t.Fatalf("NewHosts 失败: %v", err)
	}

	// "example.com" 应累积 3 条：1.2.3.4、5.6.7.8、2001:db8::1（顺序即配置序）。
	got4 := h.Lookup("example.com", TypeA)
	want4 := []string{"1.2.3.4", "5.6.7.8"}
	if len(got4) != len(want4) {
		t.Fatalf("A 查询返回 %d 个地址 %v，期望 %v", len(got4), got4, want4)
	}
	for i, ip := range got4 {
		if ip.String() != want4[i] {
			t.Errorf("A 第 %d 个地址 = %s，期望 %s", i, ip, want4[i])
		}
		if got := ip.To4(); got == nil {
			t.Errorf("A 查询的 %s 不是 4 字节形", ip)
		}
	}

	got6 := h.Lookup("example.com", TypeAAAA)
	if len(got6) != 1 || got6[0].String() != "2001:db8::1" {
		t.Errorf("AAAA 查询 = %v，期望 [2001:db8::1]", got6)
	}
	if got6[0].To4() != nil {
		t.Error("AAAA 查询返回的地址竟是 v4 可表示形")
	}

	// 规范名命中：查 "Example.COM" 与 "example.com." 都应命中。
	for _, q := range []string{"example.com", "Example.COM", "example.com."} {
		if got := h.Lookup(q, TypeA); len(got) != len(want4) {
			t.Errorf("查询 %q 返回 %v，期望命中 %d 条", q, got, len(want4))
		}
	}
	if got := h.Lookup("v6only.example.net", TypeA); got != nil {
		t.Errorf("仅有 v6 地址的域名查 A 应未命中，实际 %v", got)
	}
	if got := h.Lookup("spaced.example.org", TypeA); len(got) != 1 || got[0].String() != "9.9.9.9" {
		t.Errorf("带空白的条目 = %v，期望 [9.9.9.9]", got)
	}
}

func TestNewHostsRejectsBadSpecs(t *testing.T) {
	cases := []struct {
		spec string
		why  string
	}{
		{"", "空条目"},
		{"   ", "纯空白条目"},
		{"example.com", "缺冒号"},
		{"example.com:", "冒号后无 IP"},
		{"example.com:999.1.1.1", "非 IP 字面量"},
		{"example.com:not-an-ip", "非 IP 字面量"},
		{":1.2.3.4", "空域名"},
		{"example..com:1.2.3.4", "域名含空 label"},
		{" :1.2.3.4", "域名 trim 后为空"},
	}
	for _, c := range cases {
		if _, err := NewHosts([]string{c.spec}); err == nil {
			t.Errorf("NewHosts(%q) 应报错（%s）但通过了", c.spec, c.why)
		}
	}
	// 错误应指明是第几条坏掉（第二条 "bad" 缺冒号）。
	if _, err := NewHosts([]string{"a.com:1.1.1.1", "bad", "c.com:2.2.2.2"}); err == nil {
		t.Fatal("含坏条目的表应报错")
	} else if !strings.Contains(err.Error(), "第 2 条") {
		t.Errorf("错误信息应指明坏条目的序号，实际 %q", err.Error())
	}
}

func TestHostsLookupMissAndFamilyFilter(t *testing.T) {
	h, err := NewHosts([]string{"v4.example:1.2.3.4", "v6.example:2001:db8::2"})
	if err != nil {
		t.Fatalf("NewHosts: %v", err)
	}
	// 表里没有的域名 → nil。
	if got := h.Lookup("absent.example", TypeA); got != nil {
		t.Errorf("未登记域名应未命中，实际 %v", got)
	}
	// 地址族过滤：只有 v4 的域名查 AAAA → nil；只有 v6 的查 A → nil。
	if got := h.Lookup("v4.example", TypeAAAA); got != nil {
		t.Errorf("只有 A 条目的域名查 AAAA 应未命中，实际 %v", got)
	}
	if got := h.Lookup("v6.example", TypeA); got != nil {
		t.Errorf("只有 AAAA 条目的域名查 A 应未命中，实际 %v", got)
	}
	// A/AAAA 之外的 qtype：hosts 登记的是 IP，无从回答 → 未命中。
	for _, qt := range []uint16{TypeCNAME, TypeMX, TypeTXT, TypeSRV, TypePTR} {
		if got := h.Lookup("v4.example", qt); got != nil {
			t.Errorf("qtype=%d 应未命中，实际 %v", qt, got)
		}
	}
	// nil 表指针：查询不应 panic（Resolver.Hosts 为 nil 时也会走到这一步）。
	var nilH *Hosts
	if got := nilH.Lookup("x.com", TypeA); got != nil {
		t.Errorf("nil Hosts.Lookup = %v，期望 nil", got)
	}
}

func TestHostsLookupReturnsClone(t *testing.T) {
	h, err := NewHosts([]string{"example.com:1.2.3.4"})
	if err != nil {
		t.Fatalf("NewHosts: %v", err)
	}
	first := h.Lookup("example.com", TypeA)
	if len(first) != 1 {
		t.Fatalf("应命中 1 条，实际 %d", len(first))
	}
	first[0][0] = 0xFF // 污染调用方拿到的副本
	second := h.Lookup("example.com", TypeA)
	if second[0][0] != 1 {
		t.Errorf("Lookup 泄露了表内切片：改完第一次的返回值后，第二次拿到 %v", second[0])
	}
}

func TestHostsAnswerBuildsResponse(t *testing.T) {
	h, err := NewHosts([]string{"Example.COM:1.2.3.4"})
	if err != nil {
		t.Fatalf("NewHosts: %v", err)
	}
	h.TTL = 42 * time.Second

	req, err := NewQuery(0xBEEF, "example.com", TypeA)
	if err != nil {
		t.Fatalf("NewQuery: %v", err)
	}
	resp := h.answer(req)
	if resp == nil {
		t.Fatal("hosts 命中但 answer 返回 nil")
	}
	if resp.ID != req.ID {
		t.Errorf("应答 ID = 0x%04x，期望 0x%04x（与请求一致，数据面转发用）", resp.ID, req.ID)
	}
	if !resp.IsResponse() {
		t.Error("应答未置 QR 位")
	}
	if resp.RCode() != RCodeNoError {
		t.Errorf("应答 RCODE = %s，期望 NOERROR", rcodeName(resp.RCode()))
	}
	if resp.Flags&flagRA == 0 || resp.Flags&flagRD == 0 {
		t.Errorf("应答 FLAGS = 0x%04x，RA/RD 应置起", resp.Flags)
	}
	if len(resp.Questions) != 1 {
		t.Fatalf("问题段应有 1 条，实际 %d", len(resp.Questions))
	}
	q := resp.Questions[0]
	if q.Name != "example.com" || q.Type != TypeA || q.Class != ClassINET {
		t.Errorf("问题段未逐字回显：%+v", q)
	}
	if len(resp.Answers) != 1 {
		t.Fatalf("应答应有 1 条，实际 %d", len(resp.Answers))
	}
	rr := resp.Answers[0]
	if rr.Name != "example.com" {
		t.Errorf("RR NAME = %q，期望规范化后的 example.com", rr.Name)
	}
	if rr.Type != TypeA || rr.Class != ClassINET {
		t.Errorf("RR TYPE/CLASS = %s/%d，期望 A/IN", qtypeName(rr.Type), rr.Class)
	}
	if rr.TTL != 42 {
		t.Errorf("RR TTL = %d，期望 42", rr.TTL)
	}
	if got := rr.IP.To4(); got == nil || got.String() != "1.2.3.4" {
		t.Errorf("RR IP = %v，期望 1.2.3.4", rr.IP)
	}
	// 应答必须能独立打包/再解析（数据面拿到的是这条应答的字节）。
	packed, err := resp.Pack()
	if err != nil {
		t.Fatalf("本地应答 Pack 失败: %v", err)
	}
	reparsed, err := ParseMessage(packed)
	if err != nil {
		t.Fatalf("本地应答重新解析失败: %v", err)
	}
	if !reparsed.Answers[0].IP.Equal(net.ParseIP("1.2.3.4")) {
		t.Errorf("重解析后 IP = %v，期望 1.2.3.4", reparsed.Answers[0].IP)
	}

	// 未命中 → nil。
	if got := h.answer(mustQuery(t, 1, "absent.example", TypeA)); got != nil {
		t.Errorf("未命中域名应返回 nil 应答，实际 %+v", got)
	}
	if got := h.answer(mustQuery(t, 1, "Example.COM", TypeAAAA)); got != nil {
		t.Errorf("地址族不匹配应返回 nil 应答，实际 %+v", got)
	}
}

func TestHostsDefaultTTL(t *testing.T) {
	h, err := NewHosts([]string{"a.example:1.2.3.4"})
	if err != nil {
		t.Fatalf("NewHosts: %v", err)
	}
	// NewHosts 不显式设 TTL 时用 defaultHostsTTL。
	resp := h.answer(mustQuery(t, 1, "a.example", TypeA))
	if resp.Answers[0].TTL != uint32(defaultHostsTTL/time.Second) {
		t.Errorf("默认 TTL = %d，期望 %d", resp.Answers[0].TTL, uint32(defaultHostsTTL/time.Second))
	}
	// TTL<=0 或超上限时走夹紧路径。
	h.TTL = -1
	resp = h.answer(mustQuery(t, 1, "a.example", TypeA))
	if resp.Answers[0].TTL != uint32(defaultHostsTTL/time.Second) {
		t.Errorf("TTL<=0 时应回落默认值，实际 %d", resp.Answers[0].TTL)
	}
	h.TTL = 1000 * time.Hour
	resp = h.answer(mustQuery(t, 1, "a.example", TypeA))
	if resp.Answers[0].TTL != uint32(defaultCacheMaxTTL/time.Second) {
		t.Errorf("超大 TTL 应夹到 %d，实际 %d", uint32(defaultCacheMaxTTL/time.Second), resp.Answers[0].TTL)
	}
}

// mustQuery 是 NewQuery 的测试速记（失败即 Fatal）。
func mustQuery(t *testing.T, id uint16, name string, qtype uint16) *Message {
	t.Helper()
	q, err := NewQuery(id, name, qtype)
	if err != nil {
		t.Fatalf("NewQuery(%q, %s): %v", name, qtypeName(qtype), err)
	}
	return q
}
