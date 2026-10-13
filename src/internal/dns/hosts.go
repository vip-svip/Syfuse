package dns

// 本文件实现静态 hosts 表：形如 "example.com:1.2.3.4" 的配置条目...
// hosts 命中即由本地构造应答，不进上游、不经缓存、不分配 fake-ip。

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// defaultHostsTTL 本地构造应答用的 TTL。
const defaultHostsTTL = 60 * time.Second

// Hosts 是静态域名→IP 映射表（/etc/hosts 的配置风格）。
//
// 与上游解析的分工：hosts 是"管理员拍板的映射"，优先级压过一切网络
// 查询与缓存（见 Resolver.Query 中的顺序说明）。命中即由 answer() 本地
// 构造应答，零网络往返、零上游压力、零网络故障面 —— 内网/测试环境的
// 解析不该依赖一个可能不在的递归服务器。
type Hosts struct {
	// entries 规范域名 → IP 列表。同域名允许登记多个地址（多 A / A+AAAA），
	// 列表保序：应答里 RR 的顺序与配置顺序一致，便于对账。
	entries map[string][]net.IP

	// TTL 是本地构造应答写入的 TTL（秒）。hosts 是静态配置，TTL 只影响
	// 客户端/中间递归多久内不再来问；0 或负值用 defaultHostsTTL。
	TTL time.Duration
}

// NewHosts 从配置条目列表构造 hosts 表。
//
// 每条格式 "域名:IP"，如 "example.com:1.2.3.4" 或 "example.com:2001:db8::1"。
// 分割规则是**第一个冒号**：域名按 DNS 规则不可能含冒号，而 IPv6 字面量
// 必然含多个冒号，从右侧分割会把 "2001:db8::1" 截坏。
//
// 任何一条不合法都整体报错（宁可以构造失败暴露配置笔误，也不悄悄跳过
// 半条表 —— 那会让"某个域名莫名走了上游"查无可查）。
func NewHosts(specs []string) (*Hosts, error) {
	h := &Hosts{
		entries: make(map[string][]net.IP, len(specs)),
		TTL:     defaultHostsTTL,
	}
	for i, spec := range specs {
		if err := h.addLine(i, spec); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *Hosts) addLine(idx int, spec string) error {
	s := strings.TrimSpace(spec)
	if s == "" {
		return fmt.Errorf("dns: hosts 第 %d 条为空（期望 \"域名:IP\"，如 example.com:1.2.3.4）", idx+1)
	}
	ci := strings.Index(s, ":")
	if ci < 0 {
		return fmt.Errorf("dns: hosts 第 %d 条 %q 缺少冒号（期望 \"域名:IP\"，如 example.com:1.2.3.4）", idx+1, s)
	}
	name := s[:ci]
	ipStr := strings.TrimSpace(s[ci+1:])
	if ipStr == "" {
		return fmt.Errorf("dns: hosts 第 %d 条 %q 冒号后没有 IP", idx+1, s)
	}
	// 域名走与查询同一条规范化通道：小写化 + 校验可编码，缓存/上游路径
	// 与 hosts 路径因此用同一个键形，"Example.COM" 与 "example.com"
	// 不会分裂成两条互不相认的记录。
	// 空域名必须在这里拒绝：normalizeName("") 会返回根域 "." 且不报错
	//（根查询本身合法），但 hosts 条目是"管理员拍板的映射"，":1.2.3.4"
	// 映射到根域既无意义也会让根查询被劫持到错误地址。
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("dns: hosts 第 %d 条 %q 的域名为空（期望 \"域名:IP\"，如 example.com:1.2.3.4）", idx+1, s)
	}
	qname, err := normalizeName(name)
	if err != nil {
		return fmt.Errorf("dns: hosts 第 %d 条的域名 %q 非法：%w", idx+1, name, err)
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return fmt.Errorf("dns: hosts 第 %d 条的 IP %q 不是合法 IP 字面量", idx+1, ipStr)
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // 统一存成 4 字节，16 字节的 v4-mapped 形会让 String() 不变但字节比对不同
	}
	h.entries[qname] = append(h.entries[qname], ip)
	return nil
}

// Lookup 查静态表。返回该域名在对应地址族下的 IP 列表（克隆副本）；
// 未命中（域名不在表里、或表里没有请求地址族的地址）返回 nil。
//
// 地址族过滤语义："example.com:1.2.3.4" 配了 A 查询有结果、AAAA 查询
// 未命中。AAAA 未命中时不本地造 NODATA 而是放行上游 —— 表里没有 v6 地址
// 不代表域名真的没有 v6，上游可能有。
func (h *Hosts) Lookup(name string, qtype uint16) []net.IP {
	if h == nil {
		return nil
	}
	qname, err := normalizeName(name)
	if err != nil {
		return nil
	}
	ips := h.entries[qname]
	if len(ips) == 0 {
		return nil
	}
	var out []net.IP
	for _, ip := range ips {
		switch qtype {
		case TypeA:
			if ip.To4() != nil {
				out = append(out, cloneIP(ip))
			}
		case TypeAAAA:
			// To4()==nil 才是"纯 IPv6"：::ffff:1.2.3.4 这类 v4-mapped
			// 既是合法 v6 又能被 v4 语义解释，按 v4 归类避免一地址两族。
			if ip.To4() == nil && ip.To16() != nil {
				out = append(out, cloneIP(ip))
			}
		default:
			// hosts 登记的是 IP，对 A/AAAA 之外的 qtype 无从回答，一律未命中。
			return nil
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// answer 给查询 req 构造本地应答；未命中返回 nil。
//
// 应答按协议诚实构造：QR/RA 置起、RCODE=NOERROR、问题段逐字回显（数据面
// 把它转发给客户端时，客户端的问题段校验才能过）、A/AAAA RR 各一条。
// ID 沿用请求 ID —— 这份应答最终要经数据面回到发起查询的客户端手里，
// ID 是它对上"这是我问的那一问"的唯一凭据。
func (h *Hosts) answer(req *Message) *Message {
	if h == nil || req == nil || len(req.Questions) != 1 {
		return nil
	}
	q := req.Questions[0]
	ips := h.Lookup(q.Name, q.Type)
	if len(ips) == 0 {
		return nil
	}
	ttl := h.TTL
	if ttl <= 0 {
		ttl = defaultHostsTTL
	}
	if ttl > defaultCacheMaxTTL {
		// 与缓存上限对齐：TTL 大到几十年（uint32 秒约 136 年）没有意义，
		// 且会让客户端侧的错误映射长期不得纠正。
		ttl = defaultCacheMaxTTL
	}
	answers := make([]RR, 0, len(ips))
	for _, ip := range ips {
		answers = append(answers, RR{
			Name:  q.Name,
			Type:  q.Type,
			Class: ClassINET,
			TTL:   uint32(ttl / time.Second),
			IP:    ip,
		})
	}
	return &Message{
		ID:        req.ID,
		Flags:     flagQR | flagRA | flagRD | RCodeNoError,
		Questions: append(make([]Question, 0, 1), q),
		Answers:   answers,
	}
}
