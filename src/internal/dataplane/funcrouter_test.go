package dataplane

import (
	"testing"

	"github.com/vip-svip/syfuse/src/internal/outbound"
)

// TestNewFuncRouterRejectsNilFunc 守住构造期校验：
// 放一个 nil 函数进去，之后每次 Decide 都会走进"防御分支"，
// 而那个分支的结果（proxy）会让所有连接都走节点 —— 一次编程错误
// 变成静默的全量走代理。必须在构造时就炸。
func TestNewFuncRouterRejectsNilFunc(t *testing.T) {
	if _, err := NewFuncRouter(nil); err == nil {
		t.Fatalf("nil 决策函数必须报错")
	}
	if _, err := NewFuncRouter(nil); err != ErrNilDecideFunc {
		t.Errorf("错误应可被 errors.Is 判定为 ErrNilDecideFunc，得到 %v", err)
	}
}

// TestFuncRouterDelegatesEveryCall 决策必须每次真的问函数，
// 且把 host/port 原样传过去 —— 改一个参数名不会报错，但会让
// "按域名分流"静默失效。
func TestFuncRouterDelegatesEveryCall(t *testing.T) {
	type call struct {
		host string
		port int
	}
	var got []call
	r, err := NewFuncRouter(func(host string, port int) Decision {
		got = append(got, call{host: host, port: port})
		if host == "blocked.example" {
			return DecisionReject
		}
		return DecisionDirect
	})
	if err != nil {
		t.Fatalf("NewFuncRouter: %v", err)
	}

	cases := []struct {
		host string
		port int
		want Decision
	}{
		{"a.example", 80, DecisionDirect},
		{"blocked.example", 443, DecisionReject},
		{"b.example", 8080, DecisionDirect},
	}
	for _, c := range cases {
		if d := r.Decide(c.host, c.port); d != c.want {
			t.Errorf("Decide(%q,%d) = %v, 期望 %v", c.host, c.port, d, c.want)
		}
	}
	if len(got) != len(cases) {
		t.Fatalf("决策函数被调用 %d 次，期望 %d 次（缓存决策会漏掉规则热更新）", len(got), len(cases))
	}
	for i, c := range cases {
		if got[i].host != c.host || got[i].port != c.port {
			t.Errorf("第 %d 次调用收到 (%q,%d)，期望 (%q,%d)",
				i+1, got[i].host, got[i].port, c.host, c.port)
		}
	}
}

// TestFuncRouterNilDefensiveBranchPrefersProxy 覆盖 nil 防御分支。
//
// 取 proxy 而不是 direct 的理由写在 Decide 的注释里：fn 为 nil 只可能是
// 构造期校验被绕过，此时"多走一次节点"远好过"把本该走节点的流量放出去"。
// 这条用例把那个选择固定下来 —— 换成 direct 会红。
func TestFuncRouterNilDefensiveBranchPrefersProxy(t *testing.T) {
	var r *FuncRouter // 整型 nil：模拟构造校验被绕过的情形
	if d := r.Decide("example.com", 80); d != DecisionProxy {
		t.Errorf("nil 路由器 Decide = %v, 期望 %v（不得静默直连）", d, DecisionProxy)
	}
}

// TestFuncRouterPassesDataplaneValidation 证 FuncRouter 真的满足 Router 接口：
// Options 里放它，New 必须接受，且隧道真的按它的决策分流。
//
// 只断言"编译通过"是不够的 —— 接口里有未导出方法，外部实现根本装不进来，
// 这正是加 FuncRouter 的原因；这里补上"装进来之后行为也对"。
func TestFuncRouterPassesDataplaneValidation(t *testing.T) {
	r, err := NewFuncRouter(func(host string, port int) Decision {
		if host == "reject.example" {
			return DecisionReject
		}
		if host == "direct.example" {
			return DecisionDirect
		}
		return DecisionProxy
	})
	if err != nil {
		t.Fatalf("NewFuncRouter: %v", err)
	}

	up := newEchoUpstream(t, "BANNER")

	dp, err := New(Options{
		Listen: "127.0.0.1:0",
		Proxy:  &outbound.DirectDialer{},
		Direct: &outbound.DirectDialer{},
		Router: r,
	})
	if err != nil {
		t.Fatalf("New 拒绝 FuncRouter: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	// reject：入站必须回"规则集不允许"（0x02），不是 general failure。
	c, rep := socks5Client(t, addr, "reject.example:80")
	if rep != 0x02 {
		t.Errorf("reject.example 应答码 = 0x%02x, 期望 0x02", rep)
	}
	c.Close()

	// direct / proxy 两条都要真的把字节运过去。
	//
	// 上游给 echoUpstream，两个 Host 值都指向它：FuncRouter 决定的是
	// "走哪条出站路径"，不是"连哪个服务器"——这里正好把两者分开验证。
	for _, host := range []string{"direct.example", "proxy.example"} {
		c, rep := socks5Client(t, addr, up.addr())
		if rep != 0x00 {
			t.Fatalf("%s 应答码 = 0x%02x, 期望 0x00", host, rep)
		}
		if got := string(readWithTimeout(t, c, len("BANNER"))); got != "BANNER" {
			t.Errorf("%s banner = %q, 期望 BANNER", host, got)
		}
		c.Close()
	}
}
