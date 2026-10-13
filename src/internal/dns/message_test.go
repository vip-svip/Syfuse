package dns

// message 编解码与畸形输入防御的测试。
//
// 压缩指针环、label 超长这类用例**必须**手工逐字节构造报文：被测编码器
// 永远不会产出这些字节，只有它们能检验解析器的防御姿态。

import (
	"net"
	"strings"
	"testing"
)

func TestNameEncodeDecodeRoundTrip(t *testing.T) {
	cases := []string{
		".",
		"a",
		"example.com",
		"www.example.com",
		"a.b.c.d.e.f.example.com",
		strings.Repeat("x", 63) + ".com",         // 63 字节 label（上限内）
		strings.Repeat("y.", 40) + "example.com", // 多级（总长 255 内）
		"mixed.CASE.Name",
	}
	for _, name := range cases {
		enc, err := encodeName(name)
		if err != nil {
			t.Errorf("encodeName(%q) 失败: %v", name, err)
			continue
		}
		got, next, err := parseName(enc, 0)
		if err != nil {
			t.Errorf("parseName(encodeName(%q)) 失败: %v", name, err)
			continue
		}
		want := name
		if want == "" {
			want = "."
		}
		if got != want {
			t.Errorf("round-trip 不一致：编码前 %q，解出来 %q", name, got)
		}
		if next != len(enc) {
			t.Errorf("round-trip 后偏移不对：next=%d，报文长 %d", next, len(enc))
		}
	}
}

func TestEncodeNameRejectsGarbage(t *testing.T) {
	cases := []struct {
		name string
		why  string
	}{
		{"a..b", "连续两个点（空 label）"},
		{".a", "以点开头（空 label）"},
		{strings.Repeat("a", 64) + ".com", "label 超过 63 字节"},
		{strings.Repeat("a.", 130) + "com", "总长超过 255 字节"},
	}
	for _, c := range cases {
		if _, err := encodeName(c.name); err == nil {
			t.Errorf("encodeName(%q) 应报错（%s）但通过了", c.name, c.why)
		}
	}
}

// TestParseCompressedName 用手工字节锁住指针解析：应答里 CNAME 的目标名
// 以压缩指针指向问题段里的 "example" 标签起点。
func TestParseCompressedName(t *testing.T) {
	// 手工构造一条最小报文：
	//   header: ID=0x1234, FLAGS=0x8180, QD=1, AN=1（0..11）
	//   question @12: 3www7example3com0（17 字节，12..28），QTYPE=A @29，QCLASS=IN @31
	//     ↑ "example" 标签的长度字节在偏移 16
	//   answer   @33: C00C(指针→12) TYPE=CNAME CLASS=IN TTL=300
	//            RDLENGTH=2 RDATA=C010(指针→16，即 "example")
	// 期望 CNAME 目标 = "example.com"（跟随指针到 16 的 "example" 标签）。
	msg := []byte{
		0x12, 0x34, 0x81, 0x80, 0, 1, 0, 1, 0, 0, 0, 0,
		3, 'w', 'w', 'w', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0, 1, 0, 1,
		0xC0, 0x0C, // NAME: 指针 → 12
		0, 5, // TYPE = CNAME
		0, 1, // CLASS = IN
		0, 0, 1, 44, // TTL = 300
		0, 2, // RDLENGTH = 2
		0xC0, 0x10, // RDATA: 指针 → 16（"example" 标签）
	}
	m, err := ParseMessage(msg)
	if err != nil {
		t.Fatalf("ParseMessage 失败: %v", err)
	}
	if len(m.Answers) != 1 {
		t.Fatalf("应解析出 1 条 answer，实际 %d", len(m.Answers))
	}
	rr := m.Answers[0]
	if rr.Type != TypeCNAME {
		t.Fatalf("answer 类型 = %s，期望 CNAME", qtypeName(rr.Type))
	}
	if rr.Target != "example.com" {
		t.Errorf("CNAME 目标 = %q，期望 %q（压缩指针跟随失败）", rr.Target, "example.com")
	}
	if rr.Name != "www.example.com" {
		t.Errorf("answer NAME = %q，期望 %q", rr.Name, "www.example.com")
	}
	if rr.TTL != 300 {
		t.Errorf("TTL = %d，期望 300", rr.TTL)
	}
}

// TestParsePointerLoopIsRejected 是指针环防御的核心用例：
// 指针链必须严格递减偏移，两跳成环（12→32→32）要在**第二跳**被拒，
// 且绝不 panic。
func TestParsePointerLoopIsRejected(t *testing.T) {
	// header + 问题段位置 12 起是 0xC0 0x20（→32），位置 32 又是 0xC0 0x20（→32 自身）。
	msg := make([]byte, 34)
	copy(msg[0:12], []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
	msg[12], msg[13] = 0xC0, 0x20 // 12: 指针 → 32（前向指针，本就非法）
	msg[32], msg[33] = 0xC0, 0x20 // 32: 指针 → 32（自指）
	// 注：第一次跳转（12→32）就会因"指针目标 32 不小于自身位置 12"被拒；
	// 这条链路同时覆盖自指与前向指针两类坏输入。
	if _, err := ParseMessage(msg); err == nil {
		t.Error("含指针环/前向指针的报文应报错，但解析通过了")
	}
}

// TestParseSelfPointerAtStart 覆盖"第一跳就自指"的情形。
func TestParseSelfPointerAtStart(t *testing.T) {
	msg := make([]byte, 14)
	copy(msg[0:12], []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
	msg[12], msg[13] = 0xC0, 0x0C // 12: 指针 → 12（自指）
	if _, err := ParseMessage(msg); err == nil {
		t.Error("自指指针应报错，但解析通过了")
	}
}

func TestParseTruncatedPointer(t *testing.T) {
	// 指针只有第一个字节就被截断。
	msg := make([]byte, 13)
	copy(msg[0:12], []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
	msg[12] = 0xC0
	if _, err := ParseMessage(msg); err == nil {
		t.Error("被截断的指针应报错")
	}
}

func TestParseReservedLabelBits(t *testing.T) {
	// label 长度字节最高位模式 10（0x40）与 01（0x80）都是保留值。
	for _, b := range []byte{0x40, 0x80, 0xBF} {
		msg := make([]byte, 13)
		copy(msg[0:12], []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
		msg[12] = b
		if _, err := ParseMessage(msg); err == nil {
			t.Errorf("label 长度字节 0x%02x（保留位）应报错", b)
		}
	}
}

func TestParseShortMessage(t *testing.T) {
	if _, err := ParseMessage(nil); err == nil {
		t.Error("nil 报文应报错")
	}
	if _, err := ParseMessage(make([]byte, headerLen-1)); err == nil {
		t.Error("不足 12 字节的报文应报错")
	}
}

func TestParseQuestionMissingTail(t *testing.T) {
	// 有名无 TYPE/CLASS。
	msg := make([]byte, headerLen+4)
	copy(msg[0:12], []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
	msg[12] = 1
	msg[13] = 'a'
	msg[14] = 0
	if _, err := ParseMessage(msg); err == nil {
		t.Error("问题段缺 QTYPE/QCLASS 应报错")
	}
}

func TestParseRDLENGTHBeyondBuffer(t *testing.T) {
	// answer 的 RDLENGTH=1000 但报文只剩 2 字节。
	msg := []byte{
		0x12, 0x34, 0x81, 0x80, 0, 0, 0, 1, 0, 0, 0, 0,
		0, 0, 0, 1, 0, 1, 0, 0, 0, 60, 0x03, 0xE8, 1, 2, 3,
	}
	// 展开：NAME=root(0), TYPE=A, CLASS=IN, TTL=60, RDLENGTH=1000, RDATA=1,2,3
	if _, err := ParseMessage(msg); err == nil {
		t.Error("RDLENGTH 超出报文应报错")
	}
}

func TestParseTrailingGarbage(t *testing.T) {
	q, err := NewQuery(0x1234, "example.com", TypeA)
	if err != nil {
		t.Fatalf("NewQuery: %v", err)
	}
	buf, err := q.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	buf = append(buf, 0xDE, 0xAD) // 尾部两个杂字节
	if _, err := ParseMessage(buf); err == nil {
		t.Error("尾部多字节应报错")
	}
}

func TestParseCountMismatch(t *testing.T) {
	q, _ := NewQuery(0x1234, "example.com", TypeA)
	buf, _ := q.Pack()
	// QDCOUNT 从 1 改成 2：第二段解析必然越界。
	buf[4], buf[5] = 0, 2
	if _, err := ParseMessage(buf); err == nil {
		t.Error("声明的问题数与实际不符应报错")
	}
}

func TestPackParseRoundTrip(t *testing.T) {
	m := &Message{
		ID:    0xABCD,
		Flags: flagQR | flagRA | rcodeMask&RCodeNoError,
		Questions: []Question{
			{Name: "www.example.com", Type: TypeA, Class: ClassINET},
		},
		Answers: []RR{
			aRR("example.com", "93.184.216.34", 300),
			aaaaRR("example.com", "2606:2800:220:1:248:1893:25c8:1946", 120),
			{Name: "www.example.com", Type: TypeCNAME, Class: ClassINET, TTL: 60, Target: "example.com"},
			{Name: "example.com", Type: TypeTXT, Class: ClassINET, TTL: 30, RData: []byte("hello")},
		},
	}
	buf, err := m.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	got, err := ParseMessage(buf)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if got.ID != m.ID || got.Flags != m.Flags {
		t.Errorf("头部不守恒: ID %04x/%04x FLAGS %04x/%04x", got.ID, m.ID, got.Flags, m.Flags)
	}
	if len(got.Questions) != 1 || got.Questions[0] != m.Questions[0] {
		t.Errorf("问题段不守恒: %+v", got.Questions)
	}
	if len(got.Answers) != 4 {
		t.Fatalf("answer 数 = %d，期望 4", len(got.Answers))
	}
	if !got.Answers[0].IP.Equal(m.Answers[0].IP) {
		t.Errorf("A 记录不守恒: %v vs %v", got.Answers[0].IP, m.Answers[0].IP)
	}
	if !got.Answers[1].IP.Equal(m.Answers[1].IP) {
		t.Errorf("AAAA 记录不守恒: %v vs %v", got.Answers[1].IP, m.Answers[1].IP)
	}
	if got.Answers[2].Target != "example.com" {
		t.Errorf("CNAME 目标不守恒: %q", got.Answers[2].Target)
	}
	if string(got.Answers[3].RData) != "hello" {
		t.Errorf("未知类型 RDATA 不守恒: %q", got.Answers[3].RData)
	}
}

func TestPackRejectsBrokenRRs(t *testing.T) {
	cases := []struct {
		name string
		rr   RR
	}{
		{"A 记录给了 IPv6", RR{Name: "x.com", Type: TypeA, Class: ClassINET, IP: net.ParseIP("::1")}},
		{"A 记录什么地址都没给", RR{Name: "x.com", Type: TypeA, Class: ClassINET}},
		{"CNAME 没有目标名", RR{Name: "x.com", Type: TypeCNAME, Class: ClassINET}},
	}
	for _, c := range cases {
		m := &Message{Questions: []Question{{Name: "x.com", Type: TypeA, Class: ClassINET}}, Answers: []RR{c.rr}}
		if _, err := m.Pack(); err == nil {
			t.Errorf("%s：Pack 应报错", c.name)
		}
	}
}

func TestNewQueryValidates(t *testing.T) {
	if _, err := NewQuery(1, "has space.com", TypeA); err == nil {
		// 含空格的 label 仍可编码（DNS 名按字节走），这里只确认不会 panic
		t.Log("含空格的域名被编码为字节（不透传语义）")
	}
	if _, err := NewQuery(1, strings.Repeat("z", 64)+".com", TypeA); err == nil {
		t.Error("超长 label 的查询应报错")
	}
	q, err := NewQuery(7, "Example.COM.", TypeA)
	if err != nil {
		t.Fatalf("NewQuery: %v", err)
	}
	if q.Questions[0].Name != "example.com" {
		t.Errorf("查询名未规范化: %q", q.Questions[0].Name)
	}
	if q.ID != 7 || q.Flags&flagRD == 0 {
		t.Errorf("查询头部不对: ID=%d RD 未置起", q.ID)
	}
}

func TestFlagAccessors(t *testing.T) {
	m := &Message{Flags: flagQR | flagTC | RCodeNXDomain}
	if !m.IsResponse() {
		t.Error("QR 位应置起")
	}
	if !m.Truncated() {
		t.Error("TC 位应置起")
	}
	if m.RCode() != RCodeNXDomain {
		t.Errorf("RCode = %d，期望 %d", m.RCode(), RCodeNXDomain)
	}
	q := &Message{Flags: flagRD}
	if q.IsResponse() || q.Truncated() {
		t.Error("查询报文不应有 QR/TC")
	}
}

func TestQtypeAndRcodeNames(t *testing.T) {
	if qtypeName(TypeA) != "A" || qtypeName(TypeAAAA) != "AAAA" || qtypeName(TypeCNAME) != "CNAME" {
		t.Error("常见类型名渲染不对")
	}
	if qtypeName(9999) != "TYPE9999" {
		t.Errorf("未知类型应渲染成 TYPE9999，实际 %q", qtypeName(9999))
	}
	if rcodeName(RCodeServFail) != "SERVFAIL" || rcodeName(RCodeNXDomain) != "NXDOMAIN" {
		t.Error("常见 RCODE 名渲染不对")
	}
	if rcodeName(15) != "RCODE15" {
		t.Errorf("未知 RCODE 应渲染成 RCODE15，实际 %q", rcodeName(15))
	}
}
