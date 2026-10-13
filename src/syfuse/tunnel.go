package syfuse

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/vip-svip/syfuse/src/internal/dataplane"
	"github.com/vip-svip/syfuse/src/internal/outbound"
	"github.com/vip-svip/syfuse/src/syfuse/rule"
)

// 本文件把"配置 + 节点"变成一条**可启动的隧道参数**。
//
// 它是 Engine.Start 与 internal/dataplane 之间唯一的一层：上面是配置语义
// （路由模式、规则原文、节点 URI），下面是隧道语义（监听地址、拨号器、
// 决策函数）。两边各自演进，改任一边都不必改另一边。

// newTunnelOptions 按配置与已解析节点构造隧道参数。
//
// nodes 必须已经过 ParseNodeURI 与 SortNodes —— 本函数只消费结构，
// 不再碰 URI 文本。选中的节点是 nodes[0]（选择节点的完整机制见
// [Engine.SelectNode] 与 proxies.select）。
func newTunnelOptions(cfg *Config, nodes []Node) (dataplane.Options, error) {
	if cfg == nil {
		return dataplane.Options{}, fmt.Errorf("隧道参数: 配置为 nil")
	}

	listen := strings.TrimSpace(cfg.Listen)
	if listen == "" {
		listen = DefaultListenAddr
	}

	opts := dataplane.Options{
		Listen:      listen,
		DialTimeout: outbound.DialTimeout,
	}

	// ---- 出站拨号器 ----
	//
	// 直连模式不需要节点，但隧道构造期仍需要一个非 nil 的 Proxy 拨号器
	// （dataplane.New 的硬性要求）。此时给一个"永不应该被调用"的占位拨号器：
	// 直连模式下路由决策恒为 direct，它一旦被调用就说明决策出了问题，
	// 明确失败比静默回落直连安全。
	//
	// 注意直连模式下**即使填了节点**也用占位：直连模式的语义就是
	// "全部直连"，不因为用户恰好填了节点就把流量送出去。
	if cfg.RoutingMode == RoutingModeDirect {
		opts.Proxy = refusingDialer{reason: "直连模式下不应走代理拨号器（路由决策与此相悖）"}
	} else {
		if len(nodes) == 0 {
			return dataplane.Options{}, fmt.Errorf("路由模式 %q 需要至少一个节点", cfg.RoutingMode)
		}
		d, err := nodeDialer(nodes[0])
		if err != nil {
			return dataplane.Options{}, err
		}
		opts.Proxy = d
	}

	// ---- 路由决策 ----
	router, err := newRouter(cfg)
	if err != nil {
		return dataplane.Options{}, err
	}
	opts.Router = router

	return opts, nil
}

// nodeDialer 把节点翻译成出站拨号器。
//
// 不支持的协议**必须**报错，而不是回落到直连：回落会让
// "这个节点的协议还不支持"伪装成"节点连上了但没流量"。
func nodeDialer(n Node) (outbound.Dialer, error) {
	d, err := outbound.NewDialer(outbound.Config{
		Protocol: n.Protocol,
		Server:   n.Server,
		Port:     n.Port,
		Password: n.Password,
		Cipher:   n.Cipher,
		SNI:      n.SNI,
		Insecure: n.Insecure,
	})
	if err != nil {
		return nil, fmt.Errorf("节点 %q（协议 %q）没有可用拨号器: %w", n.Name, n.Protocol, err)
	}
	return d, nil
}

// newRouter 按路由模式构造决策器。
//
// 三种模式各给一个**显式**的决策函数，不依赖 dataplane 的隐式默认值：
// 隐式默认（Router == nil 表示全量走代理）在代码里读不出来，
// 而"什么都没配时流量往哪走"必须是看得见的。
func newRouter(cfg *Config) (dataplane.Router, error) {
	switch cfg.RoutingMode {
	case RoutingModeDirect:
		return dataplane.NewFuncRouter(func(string, int) dataplane.Decision {
			return dataplane.DecisionDirect
		})

	case RoutingModeGlobal:
		return dataplane.NewFuncRouter(func(string, int) dataplane.Decision {
			return dataplane.DecisionProxy
		})

	case RoutingModeRule:
		return newRuleRouter(cfg)
	}
	return nil, fmt.Errorf("未知路由模式 %q", cfg.RoutingMode)
}

// newRuleRouter 用**内核自带的规则引擎**做分流决策。
//
// # 与 Kotlin 侧的一致性
//
// 映射表与 `domain/engine/RuleRouter.kt` 逐条对齐 —— 同一份配置被
// 守护进程（Go）与 App（Kotlin）各自消费，两处映射不一致就会出现
// "App 显示直连、守护进程实际走了节点"这种最难查的分裂：
//
//	引擎决策    出站动作
//	BLOCK   →  REJECT   命中拦截规则，连接不得建立
//	ALLOW   →  DIRECT   命中 @@ 例外，明确放行即直连
//	REWRITE →  DIRECT   载荷是本地伪造应答，连接根本不出网
//	NONE    →  DIRECT   未命中，按 Kotlin RoutingPolicy 的默认动作
//
// NONE 的默认动作取 DIRECT 是为了与 Kotlin 侧一致（它的
// RoutingPolicy() 默认 noMatch = DIRECT）。**这不是产品层面的结论**，
// 而是"两处必须相同"的结论；把它变成用户可配的项留待配置层接线。
//
// # 规则为空时拒绝启动
//
// 连续空规则集却按规则模式跑，所有流量都会落到默认动作上，
// 用户以为分流生效了，其实没有 —— 与 dataplane.ErrNoRoutes 同一个理由。
func newRuleRouter(cfg *Config) (dataplane.Router, error) {
	text := cfg.Rules
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("规则模式（routingMode=rule）需要非空 rules；"+
			"若确实要全量走代理，请把路由模式设为 %q", RoutingModeGlobal)
	}

	// KindFilter 与 syfused rules build 用的是同一个默认类别：
	// 配置里下发的规则按过滤器语法解析（ABP 风格为主，
	// hosts/dnsmasq 行由解析器自行识别，不依赖这里的默认值）。
	report := rule.Parse(text, "config.rules", rule.KindFilter)
	if errs := report.Errors(); len(errs) > 0 {
		// 只有 ERROR 才拒绝启动。WARNING 不拦：一条写错的规则
		// 不应该让整个内核起不来。
		return nil, fmt.Errorf("rules 含 %d 条 ERROR（首条：%s）", len(errs), errs[0].Message)
	}
	eng := rule.Compile(report.NetworkRules, report.Diagnostics)
	if eng.RuleCount() == 0 {
		return nil, fmt.Errorf("rules 解析后没有得到任何可用规则；" +
			"若确实要全量走代理，请把路由模式设为 global")
	}

	return dataplane.NewFuncRouter(func(host string, _ int) dataplane.Decision {
		// 热路径约束与 Kotlin 侧一致：不收集匹配链路（collectChain=false）。
		// 连接层拿不到 url / documentHost / client / ctag，
		// 因此依赖这些维度的规则在连接层不会命中 —— 如实接受这个限制，
		// 而不是传一堆空值装作支持。
		res := eng.Match(rule.NewMatchRequest(host), false)
		switch res.Decision {
		case rule.DecisionBlock:
			return dataplane.DecisionReject
		case rule.DecisionAllow, rule.DecisionRewrite:
			return dataplane.DecisionDirect
		default: // none
			return dataplane.DecisionDirect
		}
	})
}

// ---------------------------------------------------------------------------
// 占位拨号器
// ---------------------------------------------------------------------------

// refusingDialer 永远失败的拨号器。
//
// 只用于直连模式下的 Proxy 占位。存在理由：dataplane.New 要求 Proxy
// 非 nil（防 nil 解引用），而直连模式的语义是"一条连接都不走节点"。
// 给 DirectDialer 当占位是错的 —— 万一路由决策出错，流量会静默直连出去；
// 给一个明确失败的占位，出错时是响亮的失败。
type refusingDialer struct{ reason string }

func (d refusingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	return nil, fmt.Errorf("%s（目标 %s）", d.reason, address)
}

func (d refusingDialer) Name() string { return "refuse" }
