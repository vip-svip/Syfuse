package dataplane

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Decision 是一条连接该走哪条路。
type Decision int

const (
	// DecisionProxy 走所选节点。
	DecisionProxy Decision = iota
	// DecisionDirect 直连，不过节点。
	DecisionDirect
	// DecisionReject 拒绝该连接。
	DecisionReject
)

// String 供日志与诊断使用。
func (d Decision) String() string {
	switch d {
	case DecisionDirect:
		return "direct"
	case DecisionReject:
		return "reject"
	default:
		return "proxy"
	}
}

// ParseDecision 解析配置里的动作字符串。
//
// 未知值返回错误而不是默认成 proxy：把 REJECT 拼错成"直接放行"，
// 会让本该被拦的流量静默出网。配置错误必须炸在启动时。
func ParseDecision(s string) (Decision, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "proxy", "":
		return DecisionProxy, nil
	case "direct":
		return DecisionDirect, nil
	case "reject", "block":
		return DecisionReject, nil
	default:
		return DecisionProxy, fmt.Errorf("dataplane: 未知路由动作 %q（可选 proxy / direct / reject）", s)
	}
}

// Router 对一次连接目标做路由决策。
//
// 只拿 host 与 port，**不拿**进程/UID：守护进程今天看不到发起连接的应用
// （那是 VpnService / iptables 侧的信息）。等 UID 能传进来再扩展接口，
// 而不是现在塞一个永远为空的参数假装支持。
type Router interface {
	Decide(host string, port int) Decision

	// validate 在构造期拦住"看起来配了其实没配"的空实现。
	validate() error
}

// RouteRule 是一条路由规则。
//
// # 形态选择的理由
//
// 只用"精确域名 + 域名后缀 + IP 段 + 关键字"四种，不做完整规则表达式：
// 数据面每建一条连接就要匹配一次，表达式引擎的成本会被连接数放大。
// 复杂表达式在配置层（Kotlin 侧 core:rule）编译成这四种之一再下发，
// 数据面只做匹配 —— 编译与匹配分离，两边各自可测。
type RouteRule struct {
	// Domain 精确域名（不含端口与协议）。
	Domain string `json:"domain,omitempty"`

	// Suffix 域名后缀，如 "cn" 匹配 "example.cn" 与 "a.b.example.cn"。
	Suffix string `json:"suffix,omitempty"`

	// Keyword 主机名子串，最宽的网，优先级最低。
	Keyword string `json:"keyword,omitempty"`

	// CIDR IPv4/IPv6 段，如 "10.0.0.0/8"。host 是 IP 字面量时才参与匹配。
	CIDR string `json:"cidr,omitempty"`

	// Action 命中后的动作。
	Action Decision `json:"action"`
}

// matched 具体度。数字越大越"具体"。
const (
	scoreNone    = 0
	scoreKeyword = 1
	scoreSuffix  = 2
	scoreCIDR    = 2
	scoreDomain  = 3
)

// compiledRule 是 RouteRule 的预编译形态。
//
// 预编译只做一件事：把 CIDR 字符串变成 *net.IPNet。
// 每条连接都对所有规则跑一遍，若在 Decide 里现场 ParseCIDR，
// 一万条 CIDR 规则就是每条连接一万次解析 —— 那是把 CPU 烧在解析上。
type compiledRule struct {
	rule   RouteRule
	suffix string // 去掉首尾点并小写化，避免匹配时反复处理
	ipNet  *net.IPNet
	domain string
	hasIP  bool
}

// ErrNoRoutes 规则数为 0。
//
// 规则模式下给 0 条规则是配置错误的典型症状（订阅没刷出来、
// 配置文件被截断）。此时**拒绝启动**比"看起来在跑其实什么规则都没生效"好：
// 后者会把所有流量按默认动作放行，用户还以为分流生效了。
var ErrNoRoutes = errors.New("dataplane: 规则数不能为 0（若确实要全量走默认动作，请用全局/直连模式而不是规则模式）")

// RuleRouter 按规则集合做决策。
type RuleRouter struct {
	rules []compiledRule
	def   Decision
}

// NewRuleRouter 构造规则路由器。
//
// def 是无匹配时的默认动作。rules 会被深拷贝 ——
// 调用方后续修改自己的切片不应影响已启动的隧道。
func NewRuleRouter(def Decision, rules []RouteRule) (*RuleRouter, error) {
	if len(rules) == 0 {
		return nil, ErrNoRoutes
	}
	compiled := make([]compiledRule, 0, len(rules))
	for i, ru := range rules {
		cr := compiledRule{rule: ru, domain: normalizeHost(ru.Domain)}
		if ru.Suffix != "" {
			cr.suffix = normalizeHost(ru.Suffix)
			if cr.suffix == "" {
				return nil, fmt.Errorf("dataplane: 第 %d 条规则后缀 %q 归一化后为空", i+1, ru.Suffix)
			}
		}
		if ru.CIDR != "" {
			_, ipnet, err := net.ParseCIDR(strings.TrimSpace(ru.CIDR))
			if err != nil {
				return nil, fmt.Errorf("dataplane: 第 %d 条规则 CIDR %q 非法: %w", i+1, ru.CIDR, err)
			}
			cr.ipNet = ipnet
			cr.hasIP = true
		}
		compiled = append(compiled, cr)
	}
	r := &RuleRouter{rules: compiled, def: def}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Decide 实现 Router。
//
// 取"最具体"的命中而不是"最先"命中：把精确域名放前面、后缀放后面
// 是人的写法习惯，机器不该依赖它。更具体的规则赢，给用户"顺序无所谓"的
// 自由，也避免一条宽后缀静默吃掉精确例外。
//
// 同分时**拒绝优先**：同样具体度的一条放行一条拒绝，选放行等于把用户
// 明确写了"拦"的流量放出去；选拒绝最坏是多拦一条本可放行的流量，
// 而那是可见、可在日志里报告的。安全默认值取从严。
func (r *RuleRouter) Decide(host string, port int) Decision {
	h := normalizeHost(host)

	// host 是 IP 字面量时只有 CIDR 规则能命中；域名规则跳过。
	// 反过来域名主机不参与 CIDR 匹配 —— 那是 DNS 解析层的事，
	// 数据面看到的 host 是客户端请求里写的字面量。
	var ip net.IP
	if parsed := net.ParseIP(h); parsed != nil {
		ip = parsed
	}

	best := scoreNone
	bestIdx := -1
	for i := range r.rules {
		sc := r.rules[i].score(h, ip)
		if sc == scoreNone {
			continue
		}
		if sc > best {
			best, bestIdx = sc, i
			continue
		}
		if sc == best && bestIdx >= 0 && r.rules[i].isStricter(r.rules[bestIdx]) {
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return r.def
	}
	return r.rules[bestIdx].rule.Action
}

// isStricter 同分时的取舍：拒绝赢过非拒绝。
func (cr compiledRule) isStricter(other compiledRule) bool {
	return cr.rule.Action == DecisionReject && other.rule.Action != DecisionReject
}

// score 返回该规则对 host 的具体度；未命中返回 scoreNone。
func (cr compiledRule) score(host string, ip net.IP) int {
	if cr.hasIP {
		if ip == nil || !cr.ipNet.Contains(ip) {
			return scoreNone
		}
		return scoreCIDR
	}
	if cr.domain != "" {
		// 精确匹配要求整体相等，不把"子域名也算命中"当猜测 ——
		// 猜错的后果是把 a.example.com 按 example.com 的规则处理。
		if host == cr.domain {
			return scoreDomain
		}
		return scoreNone
	}
	if cr.suffix != "" {
		if host == cr.suffix || strings.HasSuffix(host, "."+cr.suffix) {
			return scoreSuffix
		}
		return scoreNone
	}
	if cr.rule.Keyword != "" && strings.Contains(host, cr.rule.Keyword) {
		return scoreKeyword
	}
	return scoreNone
}

// validate 实现 Router：拦住四种条件全空的规则。
func (r *RuleRouter) validate() error {
	if len(r.rules) == 0 {
		return ErrNoRoutes
	}
	for i, cr := range r.rules {
		if cr.domain == "" && cr.suffix == "" && cr.rule.Keyword == "" && cr.ipNet == nil {
			return fmt.Errorf("dataplane: 第 %d 条规则四种匹配条件全为空，永远不会命中", i+1)
		}
	}
	return nil
}

// RuleCount 返回规则数，供诊断与测试。
func (r *RuleRouter) RuleCount() int { return len(r.rules) }

// normalizeHost 去掉主机名的方括号与尾点，并转小写。
//
// 尾点（"example.com."）是合法的 FQDN 写法，不去掉会让
// "example.com" 的规则匹配不上它 —— 症状是"明明配了规则却走了默认动作"，
// 极难定位。
func normalizeHost(host string) string {
	h := strings.TrimSpace(host)
	if len(h) >= 2 && h[0] == '[' && h[len(h)-1] == ']' {
		h = h[1 : len(h)-1]
	}
	h = strings.TrimSuffix(h, ".")
	return strings.ToLower(h)
}
