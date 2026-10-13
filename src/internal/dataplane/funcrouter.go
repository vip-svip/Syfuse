package dataplane

import "errors"

// ErrNilDecideFunc 构造 FuncRouter 时传入了 nil 决策函数。
var ErrNilDecideFunc = errors.New("dataplane: 决策函数不能为 nil")

// FuncRouter 用调用方提供的函数做路由决策。
//
// # 为什么需要它
//
// [Router] 接口有一个**未导出**的 validate 方法，因此外部包无法实现它 ——
// 这不是疏忽，而是为了让"构造期校验"成为必经之路（见 [NewRuleRouter]）。
// 代价是：调用方已经有一套完整规则引擎时（本仓库的 `syfuse/rule`
// 有 ABP 全语法、优先级模型、急救放行/拦截语义），要么把规则**降级**表达成
// [RouteRule] 的四种形态，要么在本包内开一个口子。
//
// 降级表达会丢语义：`$important`、`@@` 例外、正则、URL 锚定在
// "精确域名 + 后缀 + IP 段 + 关键字"里没有对应物。把引擎结果**翻译**成
// Decision 是上游的语义决策（Kotlin 侧 domain.engine.RuleRouter 就是干这个的），
// 数据面不该替它做，也不该逼它降级。
//
// 因此这里只开一个最小的口子：一个函数。规则语义、编译、优先级全部留在
// 调用方，数据面仍然只做匹配与拨号。
//
// # 与 RuleRouter 的关系
//
// 两者是**并存**的，不是替代：
//   - 配置里直接列 route 规则（四种形态）→ 用 [NewRuleRouter]；
//   - 调用方自带完整规则引擎 → 用 [NewFuncRouter] 把引擎结果换算成 Decision。
type FuncRouter struct {
	decide func(host string, port int) Decision
}

// NewFuncRouter 用函数构造路由器。fn 为 nil 时返回 [ErrNilDecideFunc]。
func NewFuncRouter(fn func(host string, port int) Decision) (*FuncRouter, error) {
	if fn == nil {
		return nil, ErrNilDecideFunc
	}
	return &FuncRouter{decide: fn}, nil
}

// Decide 每次连接调用一次。
//
// nil 防御分支取 DecisionProxy 而非 Direct：fn 为 nil 只可能是构造期
// 校验被绕过（编程错误），此时"多走一次节点"的代价远小于"把本该走节点的
// 流量静默直连出去"。这条分支由 validate 守着，正常路径永不发生。
func (r *FuncRouter) Decide(host string, port int) Decision {
	if r == nil || r.decide == nil {
		return DecisionProxy
	}
	return r.decide(host, port)
}

func (r *FuncRouter) validate() error {
	if r == nil || r.decide == nil {
		return ErrNilDecideFunc
	}
	return nil
}
