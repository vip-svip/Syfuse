package io.github.vipsvip.syfuse.domain.engine

import com.google.common.truth.Truth.assertThat
import io.github.vipsvip.syfuse.core.rule.Decision
import io.github.vipsvip.syfuse.core.rule.FilterEngine
import io.github.vipsvip.syfuse.core.rule.Priority
import io.github.vipsvip.syfuse.core.rule.ResourceType
import io.github.vipsvip.syfuse.core.rule.RuleParser
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

/**
 * [RuleRouter] 单元测试：内联规则集，不依赖 tests/corpus
 * （语料由另一个并行单元维护，本类与之解耦；语料侧证据见 [CorpusRouteMappingTest]）。
 *
 * 纪律：默认策略取 `PROXY("默认节点")` 而不是 `DIRECT` —— 否则"未命中走默认"
 * 与"放行规则命中"在动作上都是 DIRECT，两者将无法区分，测试会漏掉
 * "未匹配被静默当成直连"这一类缺陷。
 */
class RuleRouterTest {

    private fun engineOf(vararg lines: String): FilterEngine {
        val report = RuleParser.parse(lines.joinToString("\n"), sourceId = "unit")
        assertThat(report.errors).isEmpty()
        return FilterEngine.compile(report.networkRules, report.diagnostics)
    }

    /** 默认走代理的 router：未命中必须现形为 PROXY，不能混进 DIRECT。 */
    private fun proxyRouter(vararg lines: String): RuleRouter =
        RuleRouter(engineOf(*lines), RoutingPolicy(DefaultAction.PROXY("默认节点")))

    /** 默认直连的 router。 */
    private fun directRouter(vararg lines: String): RuleRouter =
        RuleRouter(engineOf(*lines), RoutingPolicy(DefaultAction.DIRECT))

    @Test
    fun `域名锚定规则命中连接目标时拒绝出网`() {
        val router = proxyRouter("||block.test^")

        assertThat(router.decide(RoutingTarget("block.test")).action).isEqualTo(RouteAction.REJECT)
        // 子域同样命中：域名锚定是后缀匹配
        assertThat(router.decide(RoutingTarget("sub.block.test")).action).isEqualTo(RouteAction.REJECT)
        // 后缀越界不得命中：更长的无关域名不能被锚定拖下水
        assertThat(router.decide(RoutingTarget("block.test.evil.test")).action).isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `例外放行规则命中时直连`() {
        val router = proxyRouter("@@||allow.test^", "||allow.test^")

        val decision = router.decide(RoutingTarget("allow.test"))

        assertThat(decision.action).isEqualTo(RouteAction.DIRECT)
        assertThat(decision.engineDecision).isEqualTo(Decision.ALLOW)
        // 同级下普通放行（rank 2）压过普通拦截（rank 1），即使拦截规则写在前面
        assertThat(decision.enginePriority).isEqualTo(Priority.NORMAL_ALLOW)
        assertThat(decision.matchedRule?.raw).isEqualTo("@@||allow.test^")
        assertThat(decision.node).isNull()
    }

    @Test
    fun `普通放行不敌 important 拦截`() {
        val router = proxyRouter("@@||mix.test^", "||mix.test^${'$'}important")

        val decision = router.decide(RoutingTarget("mix.test"))

        assertThat(decision.action).isEqualTo(RouteAction.REJECT)
        assertThat(decision.enginePriority).isEqualTo(Priority.IMPORTANT_BLOCK)
    }

    @Test
    fun `important 放行盖过 important 拦截`() {
        val router = proxyRouter("@@||imp.test^${'$'}important", "||imp.test^${'$'}important")

        val decision = router.decide(RoutingTarget("imp.test"))

        assertThat(decision.action).isEqualTo(RouteAction.DIRECT)
        assertThat(decision.enginePriority).isEqualTo(Priority.IMPORTANT_ALLOW)
    }

    @Test
    fun `URL 锚定规则需要完整 URL 参与匹配`() {
        val router = proxyRouter("https://cdn.test/js/tracker.js")

        // HTTP 层能给出完整 URL：命中
        val withUrl = router.decide(
            RoutingTarget(host = "cdn.test", url = "https://cdn.test/js/tracker.js"),
        )
        assertThat(withUrl.action).isEqualTo(RouteAction.REJECT)

        // 连接层拿不到 URL：同一域名不得被 URL 锚定规则命中（否则整站被误伤）
        val withoutUrl = router.decide(RoutingTarget("cdn.test"))
        assertThat(withoutUrl.action).isEqualTo(RouteAction.PROXY)
        assertThat(withoutUrl.engineDecision).isEqualTo(Decision.NONE)
    }

    @Test
    fun `通配符与正则规则命中`() {
        val router = proxyRouter("wild-*.com", "ads?banner\\d+/")

        assertThat(router.decide(RoutingTarget("wild-a.com")).action).isEqualTo(RouteAction.REJECT)
        // 顶级域不符：通配符不是"什么都行"
        assertThat(router.decide(RoutingTarget("wild-a.test")).action).isEqualTo(RouteAction.PROXY)
        // 正则只对完整 URL 求值
        assertThat(
            router.decide(
                RoutingTarget(host = "site.test", url = "https://site.test/adsbanner12/x"),
            ).action,
        ).isEqualTo(RouteAction.REJECT)
    }

    @Test
    fun `hosts 黑洞地址命中即拒绝`() {
        val router = proxyRouter(
            "0.0.0.0 hosts-zero.test",
            "127.0.0.1 hosts-loop.test",
            "::1 hosts-v6.test",
        )

        assertThat(router.decide(RoutingTarget("hosts-zero.test")).action).isEqualTo(RouteAction.REJECT)
        assertThat(router.decide(RoutingTarget("hosts-loop.test")).action).isEqualTo(RouteAction.REJECT)
        assertThat(router.decide(RoutingTarget("hosts-v6.test")).action).isEqualTo(RouteAction.REJECT)
    }

    @Test
    fun `hosts 非黑洞地址是重写而不是拦截`() {
        val router = proxyRouter("1.2.3.4 rewrite.test")

        val decision = router.decide(RoutingTarget("rewrite.test"))

        assertThat(decision.action).isEqualTo(RouteAction.DIRECT)
        assertThat(decision.engineDecision).isEqualTo(Decision.REWRITE)
        // 重写载荷必须对上层可见，否则 M6 的 DNS 层无从消费
        assertThat(decision.dnsRewrite).isEqualTo("1.2.3.4")
        assertThat(decision.reason).contains("重写")
    }

    @Test
    fun `dnsmasq 三种形态各自映射`() {
        val router = proxyRouter(
            "address=/dq-block.test/",
            "address=/dq-rw.test/9.9.9.9",
            "local=/dq-local.test/",
        )

        assertThat(router.decide(RoutingTarget("dq-block.test")).action).isEqualTo(RouteAction.REJECT)

        val rewrite = router.decide(RoutingTarget("dq-rw.test"))
        assertThat(rewrite.action).isEqualTo(RouteAction.DIRECT)
        assertThat(rewrite.dnsRewrite).isEqualTo("9.9.9.9")

        val local = router.decide(RoutingTarget("dq-local.test"))
        assertThat(local.action).isEqualTo(RouteAction.DIRECT)
        assertThat(local.dnsRewrite).isEqualTo("local")
    }

    @Test
    fun `IPv4 网段规则命中字面量 IP`() {
        val router = proxyRouter("10.10.0.0/16")

        assertThat(router.decide(RoutingTarget("10.10.5.5")).action).isEqualTo(RouteAction.REJECT)
        // 网段外 → 未命中 → 默认动作
        assertThat(router.decide(RoutingTarget("11.10.5.5")).action).isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `domain 作用域限定规则适用范围`() {
        val router = proxyRouter("||scoped.test^${'$'}domain=site-a.test")

        assertThat(
            router.decide(RoutingTarget("scoped.test", documentHost = "site-a.test")).action,
        ).isEqualTo(RouteAction.REJECT)
        // 来源域名不在白名单 → 规则不适用 → 默认动作
        assertThat(
            router.decide(RoutingTarget("scoped.test", documentHost = "other.test")).action,
        ).isEqualTo(RouteAction.PROXY)
        // 连接层拿不到来源页面（documentHost=null）：include 非空时不得放行规则生效
        assertThat(router.decide(RoutingTarget("scoped.test")).action).isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `domain 排除写法命中未排除的来源`() {
        val router = proxyRouter("||scoped-ex.test^${'$'}domain=~site-a.test")

        assertThat(
            router.decide(RoutingTarget("scoped-ex.test", documentHost = "site-a.test")).action,
        ).isEqualTo(RouteAction.PROXY)
        assertThat(
            router.decide(RoutingTarget("scoped-ex.test", documentHost = "other.test")).action,
        ).isEqualTo(RouteAction.REJECT)
    }

    @Test
    fun `script 资源类型修饰符在拿得到类型时生效`() {
        val router = proxyRouter("||typed.test^${'$'}script", "||typed-all.test^")

        assertThat(
            router.decide(RoutingTarget("typed.test", resourceType = ResourceType.SCRIPT)).action,
        ).isEqualTo(RouteAction.REJECT)
        assertThat(
            router.decide(RoutingTarget("typed.test", resourceType = ResourceType.IMAGE)).action,
        ).isEqualTo(RouteAction.PROXY)
        // 连接层拿不到资源类型（null → other）：不得命中 $script 规则
        assertThat(router.decide(RoutingTarget("typed.test")).action).isEqualTo(RouteAction.PROXY)
        // 未限定资源类型 → 任何类型都适用
        assertThat(
            router.decide(RoutingTarget("typed-all.test", resourceType = ResourceType.IMAGE)).action,
        ).isEqualTo(RouteAction.REJECT)
    }

    @Test
    fun `denyallow 例外与排除并存`() {
        val router = proxyRouter("@@||deny.test^${'$'}denyallow=blocked.deny.test")

        assertThat(router.decide(RoutingTarget("deny.test")).action).isEqualTo(RouteAction.DIRECT)
        assertThat(router.decide(RoutingTarget("blocked.deny.test")).action).isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `badfilter 关闭同形规则后回到默认动作`() {
        val router = proxyRouter("||bad.test^", "||bad.test^${'$'}badfilter")

        val decision = router.decide(RoutingTarget("bad.test"))

        assertThat(decision.engineDecision).isEqualTo(Decision.NONE)
        assertThat(decision.action).isEqualTo(RouteAction.PROXY)
        assertThat(decision.reason).contains("未命中任何规则")
    }

    @Test
    fun `client 作用域按连接归属应用匹配`() {
        val router = proxyRouter("||client.test^${'$'}client=syfuse")

        assertThat(router.decide(RoutingTarget("client.test", client = "syfuse")).action)
            .isEqualTo(RouteAction.REJECT)
        assertThat(router.decide(RoutingTarget("client.test", client = "other-app")).action)
            .isEqualTo(RouteAction.PROXY)
        // client=null（M9 前的常态）：规则不适用，而不是当成任意应用
        assertThat(router.decide(RoutingTarget("client.test")).action).isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `ctag 作用域按客户端标签匹配`() {
        val router = proxyRouter("||ctag.test^${'$'}ctag=ios")

        assertThat(router.decide(RoutingTarget("ctag.test", ctag = "ios")).action)
            .isEqualTo(RouteAction.REJECT)
        assertThat(router.decide(RoutingTarget("ctag.test", ctag = "android")).action)
            .isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `未命中时按显式默认动作直连`() {
        val router = directRouter("||block.test^")

        val decision = router.decide(RoutingTarget("unrelated.test"))

        assertThat(decision.action).isEqualTo(RouteAction.DIRECT)
        assertThat(decision.engineDecision).isEqualTo(Decision.NONE)
        // "没规则管它"必须自己说出来，而不是伪装成一次规则命中
        assertThat(decision.reason).contains("未命中任何规则")
        assertThat(decision.matchedRule).isNull()
    }

    @Test
    fun `未命中时按显式默认动作走代理`() {
        val router = proxyRouter("||block.test^")

        val decision = router.decide(RoutingTarget("unrelated.test"))

        assertThat(decision.action).isEqualTo(RouteAction.PROXY)
        assertThat(decision.node).isEqualTo("默认节点")
        assertThat(decision.reason).contains("默认节点")
    }

    @Test
    fun `默认代理但尚未选择节点时保持 PROXY 而不是静默直连`() {
        val router = RuleRouter(
            engineOf("||block.test^"),
            RoutingPolicy(DefaultAction.PROXY(node = null)),
        )

        val decision = router.decide(RoutingTarget("unrelated.test"))

        // 关键断言：没有节点可选时也**不是** DIRECT —— 静默改判直连会泄漏流量
        assertThat(decision.action).isEqualTo(RouteAction.PROXY)
        assertThat(decision.node).isNull()
        assertThat(decision.reason).contains("尚未选择节点")
    }

    @Test
    fun `每个决策都带非空可读原因`() {
        val router = proxyRouter("||block.test^", "@@||allow.test^", "1.2.3.4 rw.test")

        val decisions = listOf(
            router.decide(RoutingTarget("block.test")),
            router.decide(RoutingTarget("allow.test")),
            router.decide(RoutingTarget("rw.test")),
            router.decide(RoutingTarget("unrelated.test")),
        )

        for (decision in decisions) {
            assertThat(decision.reason).isNotEmpty()
        }
        // PROXY 之外的动议不得携带节点名，避免上层误用
        for (decision in decisions.filter { it.action != RouteAction.PROXY }) {
            assertThat(decision.node).isNull()
        }
    }

    @Test
    fun `端口字段当前不参与规则匹配`() {
        val router = proxyRouter("||block.test^")

        // 端口只随连接描述带入，规则语法没有端口修饰符：换端口不改变结论
        assertThat(router.decide(RoutingTarget("block.test", port = 443)).action)
            .isEqualTo(RouteAction.REJECT)
        assertThat(router.decide(RoutingTarget("block.test", port = 8080)).action)
            .isEqualTo(RouteAction.REJECT)
        // 端口本身不能触发任何规则
        assertThat(router.decide(RoutingTarget("other.test", port = 443)).action)
            .isEqualTo(RouteAction.PROXY)
    }

    @Test
    fun `空 host 的连接目标直接抛错而不是问规则`() {
        // 上层解析不出目标（如畸形 URL）时必须如实报错，
        // 而不是造一个空 host 让引擎"正常"返回未命中。
        assertThrows(IllegalArgumentException::class.java) { RoutingTarget(host = "") }
    }
}
