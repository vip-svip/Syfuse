// Package leak 实现节点与 IP 防泄漏检测（M12）。
//
// 设计要点：
//   - 检测维度与《第 5.8 节点与 IP 防泄漏》一一对应：dns / ipv6 / stun /
//     quic / ip_echo / split / system 七个维度。
//   - 所有网络探测通过 Probe 接口注入。生产环境用真实探针（probe.go），
//     测试注入假探针得到确定性结论 —— 检测逻辑本身不做任何 I/O。
//   - 每个 RunResult 携带 Status / Detail / Evidence（原始探测证据），
//     面板据此渲染"结论 + 证据"；Fix 给出一键修复建议文案。
package leak

import (
	"fmt"
	"strings"
	"time"
)

// Status 是单个维度检测的结论。
type Status string

const (
	// Pass 表示该维度无泄漏。
	Pass Status = "pass"
	// Warn 表示存在可疑迹象但未确认泄漏（如部分请求走了直连）。
	Warn Status = "warn"
	// Fail 表示确认泄漏。
	Fail Status = "fail"
	// Skip 表示该维度被配置关闭或探测不可用，不参与结论。
	Skip Status = "skip"
)

// RunResult 是一次检测中单个维度的结果。
type RunResult struct {
	Dimension string `json:"dimension"` // dns/ipv6/stun/quic/ip_echo/split/system
	Status    Status `json:"status"`
	Detail    string `json:"detail"`     // 人读结论
	Evidence  string `json:"evidence"`   // 原始探测证据（请求/响应摘要）
	Fix       string `json:"fix"`        // 一键修复建议；为空表示无需修复
	CheckedAt string `json:"checked_at"` // RFC3339
}

// Report 是一次完整自检的报告，可直接 JSON 导出。
type Report struct {
	StartedAt string      `json:"started_at"`
	Results   []RunResult `json:"results"`
	// Overall 是汇总结论：任一 fail → "leak"；任一 warn → "suspicious"；
	// 其余 → "safe"。
	Overall string `json:"overall"`
}

// Config 控制本次检测跑哪些维度。
// 全部维度的开关都显式可配（false = Skip），由调用方（UDS 方法/UI）
// 从用户配置翻译而来；这里不读任何全局配置。
type Config struct {
	CheckDNS    bool
	CheckIPv6   bool
	CheckSTUN   bool
	CheckQUIC   bool
	CheckIPEcho bool
	CheckSplit  bool
	CheckSystem bool

	// IPEchoEndpoints 是真实 IP 回显端点列表（可自定义）；
	// 为空用默认端点。仅在 CheckIPEcho=true 时使用。
	IPEchoEndpoints []string
	// STUNServers 是 STUN 服务器列表；为空用默认。
	STUNServers []string
	// SensitiveDomains 是分流泄漏检测关心的敏感域名后缀（银行/支付/邮箱）。
	SensitiveDomains []string
}

// Probe 抽象全部网络探测。每个方法返回 (结果, error)；
// error 一律按"探测不可用"处理（对应维度记 Skip/Warn），不 panic。
type Probe interface {
	// DNSUpstream 返回当前 DNS 解析实际使用的上游标识
	//（例如 "tun-doh" 或 "isp:192.168.1.1"）。无法判定时返回 error。
	DNSUpstream() (string, error)
	// ResolveAAAA 探测域名是否解析出 AAAA 记录（IPv6 直连风险）。
	ResolveAAAA(host string) (hasAAAA bool, err error)
	// STUNBinding 通过给定 STUN 服务器探测 UDP 出口 IP，
	// 返回观察到的出口 IP。失败返回 error。
	STUNBinding(server string) (observedIP string, err error)
	// QUICReachable 探测 UDP 443（QUIC）能否直连。
	QUICReachable() (bool, error)
	// PublicIP 通过给定回显端点获取出口 IP。
	PublicIP(endpoint string) (string, error)
	// RouteFor 返回目标域名当前的实际路由决策（"proxy"/"direct"/"reject"）。
	RouteFor(domain string) (string, error)
	// SystemFlags 返回系统级状态：私有 DNS 是否启用、热点共享是否开启、
	// captive portal 是否未通过。key 取值见 System* 常量。
	SystemFlags() (map[string]bool, error)
}

// 系统级状态标志的键。
const (
	SysPrivateDNS    = "private_dns_on"
	SysTetheringOn   = "tethering_on"
	SysCaptivePortal = "captive_portal"
)

// 默认探测参数（可被 Config 覆盖）。
var (
	DefaultIPEchoEndpoints = []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}
	DefaultSTUNServers = []string{
		"stun.l.google.com:19302",
		"stun.cloudflare.com:3478",
	}
	DefaultSensitiveDomains = []string{
		"bank.", "pay.", "wallet.", "mail.",
	}
)

// Checker 持有探针，反复 Run 生成报告。
type Checker struct {
	probe Probe
	now   func() time.Time
}

// NewChecker 构造检测器；probe 不能为 nil。
func NewChecker(probe Probe) (*Checker, error) {
	if probe == nil {
		return nil, fmt.Errorf("leak: probe 不能为 nil")
	}
	return &Checker{probe: probe, now: time.Now}, nil
}

// SetClock 仅供测试注入时钟。
func (c *Checker) SetClock(now func() time.Time) { c.now = now }

// Run 执行一轮全维度检测，生成报告。
func (c *Checker) Run(cfg Config) Report {
	rep := Report{StartedAt: c.now().UTC().Format(time.RFC3339)}
	if cfg.CheckDNS {
		rep.Results = append(rep.Results, c.checkDNS())
	}
	if cfg.CheckIPv6 {
		rep.Results = append(rep.Results, c.checkIPv6())
	}
	if cfg.CheckSTUN {
		for _, r := range c.checkSTUN(cfg.STUNServers) {
			rep.Results = append(rep.Results, r)
		}
	}
	if cfg.CheckQUIC {
		rep.Results = append(rep.Results, c.checkQUIC())
	}
	if cfg.CheckIPEcho {
		for _, r := range c.checkIPEcho(cfg.IPEchoEndpoints) {
			rep.Results = append(rep.Results, r)
		}
	}
	if cfg.CheckSplit {
		for _, r := range c.checkSplit(cfg.SensitiveDomains) {
			rep.Results = append(rep.Results, r)
		}
	}
	if cfg.CheckSystem {
		rep.Results = append(rep.Results, c.checkSystem())
	}
	for i := range rep.Results {
		if rep.Results[i].CheckedAt == "" {
			rep.Results[i].CheckedAt = c.now().UTC().Format(time.RFC3339)
		}
	}
	rep.Overall = Overall(rep.Results)
	return rep
}

// Overall 从结果集汇总整体结论。
func Overall(results []RunResult) string {
	overall := "safe"
	for _, r := range results {
		switch r.Status {
		case Fail:
			return "leak"
		case Warn:
			overall = "suspicious"
		}
	}
	return overall
}

func (c *Checker) stamp(r RunResult) RunResult {
	r.CheckedAt = c.now().UTC().Format(time.RFC3339)
	return r
}

// checkDNS：解析上游必须是隧道内加密上游；出现运营商/局域网上游即泄漏。
func (c *Checker) checkDNS() RunResult {
	r := RunResult{Dimension: "dns"}
	up, err := c.probe.DNSUpstream()
	if err != nil {
		r.Status = Skip
		r.Detail = "无法判定 DNS 上游: " + err.Error()
		return c.stamp(r)
	}
	r.Evidence = "upstream=" + up
	switch classifyUpstream(up) {
	case "tunnel":
		r.Status = Pass
		r.Detail = "DNS 走隧道内加密上游"
	default:
		r.Status = Fail
		r.Detail = "DNS 解析疑似来自本地/运营商上游（泄漏）"
		r.Fix = "开启「DNS 强制走隧道」，禁用系统 Private DNS，上游改用 DoH/DoT"
	}
	return c.stamp(r)
}

// classifyUpstream 把上游标识归类为 tunnel / local。
// 隧道内上游约定以 "tun" 前缀或含 "doh"/"dot"/"doq"；其余视为本地。
func classifyUpstream(up string) string {
	switch {
	case len(up) >= 3 && up[:3] == "tun",
		containsAny(up, "doh", "dot", "doq"):
		return "tunnel"
	default:
		return "local"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// checkIPv6：存在 AAAA 解析路径即提示风险（按规范"全阻断或全走隧道"）。
func (c *Checker) checkIPv6() RunResult {
	r := RunResult{Dimension: "ipv6"}
	const probeHost = "ipv6.test"
	has, err := c.probe.ResolveAAAA(probeHost)
	if err != nil {
		r.Status = Skip
		r.Detail = "无法探测 AAAA: " + err.Error()
		return c.stamp(r)
	}
	r.Evidence = fmt.Sprintf("AAAA(%s)=%v", probeHost, has)
	if has {
		r.Status = Warn
		r.Detail = "存在 AAAA 解析，需确认 IPv6 全部走隧道或阻断"
		r.Fix = "开启「IPv6 全阻断」或在分流中让 IPv6 全走隧道"
	} else {
		r.Status = Pass
		r.Detail = "无 AAAA 直连路径"
	}
	return c.stamp(r)
}

// checkSTUN：逐个 STUN 服务器探测；任一失败（不可达）是好事，
// 全部可达且返回出口 IP 则 Warn（UDP 出口可被用来回显真实 IP）。
func (c *Checker) checkSTUN(servers []string) []RunResult {
	if len(servers) == 0 {
		servers = DefaultSTUNServers
	}
	var out []RunResult
	for _, srv := range servers {
		r := RunResult{Dimension: "stun"}
		ip, err := c.probe.STUNBinding(srv)
		if err != nil {
			r.Status = Pass
			r.Detail = "STUN 不可达（UDP 直连被阻断）"
			r.Evidence = srv + ": blocked"
		} else {
			r.Status = Warn
			r.Detail = "STUN 可达，UDP 出口 IP 可被观测: " + ip
			r.Evidence = srv + ": " + ip
			r.Fix = "开启「WebRTC/STUN 阻断」（防火墙阻断 UDP 3478/19302 或 Hook 阻断）"
		}
		out = append(out, c.stamp(r))
	}
	return out
}

// checkQUIC：QUIC 可直连 = UDP 443 未受管，Warn。
func (c *Checker) checkQUIC() RunResult {
	r := RunResult{Dimension: "quic"}
	ok, err := c.probe.QUICReachable()
	if err != nil {
		r.Status = Skip
		r.Detail = "无法探测 QUIC: " + err.Error()
		return c.stamp(r)
	}
	r.Evidence = fmt.Sprintf("udp443_reachable=%v", ok)
	if ok {
		r.Status = Warn
		r.Detail = "QUIC（UDP 443）可直连，可能绕过隧道"
		r.Fix = "开启「QUIC 阻断」或让 UDP 全量走隧道"
	} else {
		r.Status = Pass
		r.Detail = "QUIC 直连被阻断"
	}
	return c.stamp(r)
}

// checkIPEcho：多个回显端点比对出口 IP。
// 混合出口（两个以上不同 IP）= Fail；单一出口 = Pass（是否与节点一致
// 由调用方拿节点预期地区比对，这里只负责把观测值带在 Evidence 里）。
func (c *Checker) checkIPEcho(endpoints []string) []RunResult {
	if len(endpoints) == 0 {
		endpoints = DefaultIPEchoEndpoints
	}
	var out []RunResult
	seen := map[string]string{} // ip -> endpoint
	var ips []string
	for _, ep := range endpoints {
		r := RunResult{Dimension: "ip_echo"}
		ip, err := c.probe.PublicIP(ep)
		if err != nil {
			r.Status = Skip
			r.Detail = "回显端点不可用: " + err.Error()
			r.Evidence = ep
			out = append(out, c.stamp(r))
			continue
		}
		r.Evidence = ep + " -> " + ip
		if _, dup := seen[ip]; !dup {
			seen[ip] = ep
			ips = append(ips, ip)
		}
		r.Status = Pass
		r.Detail = "出口 IP=" + ip
		if len(ips) > 1 {
			r.Status = Fail
			r.Detail = fmt.Sprintf("检测到混合出口（%d 个不同 IP），存在分流泄漏", len(ips))
			r.Fix = "检查分流规则，把回显域名加入代理；关闭「允许直连回退」"
		}
		out = append(out, c.stamp(r))
	}
	return out
}

// checkSplit：敏感域名（银行/支付/邮箱）若路由为 direct 则告警。
func (c *Checker) checkSplit(domains []string) []RunResult {
	if len(domains) == 0 {
		domains = DefaultSensitiveDomains
	}
	var out []RunResult
	for _, d := range domains {
		r := RunResult{Dimension: "split"}
		route, err := c.probe.RouteFor(d)
		if err != nil {
			r.Status = Skip
			r.Detail = "无法获取路由: " + err.Error()
			r.Evidence = d
			out = append(out, c.stamp(r))
			continue
		}
		r.Evidence = d + " -> " + route
		if route == "direct" {
			r.Status = Warn
			r.Detail = "敏感域名 " + d + " 当前走直连，存在分流泄漏风险"
			r.Fix = "把 " + d + " 加入代理分流，或开启敏感域名告警"
		} else {
			r.Status = Pass
			r.Detail = d + " 路由正常（" + route + "）"
		}
		out = append(out, c.stamp(r))
	}
	return out
}

// checkSystem：私有 DNS / 热点 / captive portal 系统级状态。
func (c *Checker) checkSystem() RunResult {
	r := RunResult{Dimension: "system"}
	flags, err := c.probe.SystemFlags()
	if err != nil {
		r.Status = Skip
		r.Detail = "无法读取系统状态: " + err.Error()
		return c.stamp(r)
	}
	r.Evidence = fmt.Sprintf("private_dns=%v tethering=%v captive=%v",
		flags[SysPrivateDNS], flags[SysTetheringOn], flags[SysCaptivePortal])
	switch {
	case flags[SysPrivateDNS]:
		r.Status = Warn
		r.Detail = "系统 Private DNS 开启，可能绕过隧道 DNS"
		r.Fix = "关闭系统 Private DNS 或由模块覆盖"
	case flags[SysCaptivePortal]:
		r.Status = Warn
		r.Detail = "Captive Portal 未通过，网络可能未真正就绪"
	default:
		r.Status = Pass
		r.Detail = "系统级状态正常"
	}
	return c.stamp(r)
}
