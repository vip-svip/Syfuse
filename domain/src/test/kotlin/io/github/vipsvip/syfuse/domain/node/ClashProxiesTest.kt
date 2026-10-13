package io.github.vipsvip.syfuse.domain.node

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test

/**
 * Clash YAML 订阅解析测试。
 *
 * 夹具取自常见导出形态（Clash for Windows / ClashX Meta / 自建面板），
 * 而不是我凭空构造的理想格式 —— 前两者大量使用单行 flow（`ws-opts: {…}`），
 * 后者常用缩进块。两种都必须认。
 */
class ClashProxiesTest {

    // -----------------------------------------------------------------------
    // 形态识别
    // -----------------------------------------------------------------------

    @Test
    fun `解析 ss vmess trojan 三类基本节点`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "香港 01"
                type: ss
                server: hk1.example.com
                port: 8388
                cipher: aes-256-gcm
                password: "secret pw"
              - name: "日本 02"
                type: vmess
                server: jp2.example.com
                port: 443
                uuid: b831381d-6324-4d53-ad4f-8cda48b30811
                alterId: 0
                cipher: auto
                tls: true
              - name: "美国 03"
                type: trojan
                server: us3.example.com
                port: 443
                password: tpw
            """.trimIndent()
        )

        assertThat(nodes).hasSize(3)
        assertThat(nodes[0].protocolRaw).isEqualTo("ss")
        assertThat(nodes[0].server).isEqualTo("hk1.example.com")
        assertThat(nodes[0].port).isEqualTo(8388)
        // 引号内含空格的密码必须完整保留
        assertThat(nodes[0].password).isEqualTo("secret pw")
        assertThat(nodes[0].name).isEqualTo("香港 01")

        assertThat(nodes[1].protocolRaw).isEqualTo("vmess")
        assertThat(nodes[1].uuid).isEqualTo("b831381d-6324-4d53-ad4f-8cda48b30811")
        assertThat(nodes[1].tls).isTrue()

        assertThat(nodes[2].protocolRaw).isEqualTo("trojan")
        assertThat(nodes[2].password).isEqualTo("tpw")
    }

    @Test
    fun `单行 flow 的 ws-opts 被解析成传输参数`() {
        // ClashX Meta 的典型导出形态
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "ws 节点"
                type: vmess
                server: a.example.com
                port: 443
                uuid: b831381d-6324-4d53-ad4f-8cda48b30811
                network: ws
                ws-opts: {path: /ray, headers: {Host: cdn.example.com}}
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        val n = nodes[0]
        assertThat(n.network).isEqualTo("ws")
        assertThat(n.path).isEqualTo("/ray")
        assertThat(n.host).isEqualTo("cdn.example.com")
    }

    @Test
    fun `缩进块形式的 ws-opts 同样解析`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "ws 节点"
                type: vless
                server: a.example.com
                port: 443
                uuid: b831381d-6324-4d53-ad4f-8cda48b30811
                network: ws
                ws-opts:
                  path: /vless
                  headers:
                    Host: cdn2.example.com
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].path).isEqualTo("/vless")
        assertThat(nodes[0].host).isEqualTo("cdn2.example.com")
    }

    @Test
    fun `grpc 的 service-name 进入 path`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "grpc 节点"
                type: vless
                server: g.example.com
                port: 443
                uuid: b831381d-6324-4d53-ad4f-8cda48b30811
                network: grpc
                grpc-opts:
                  grpc-service-name: gsvc
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].path).isEqualTo("gsvc")
    }

    @Test
    fun `ss 的 plugin 与 plugin-opts 进入混淆字段`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "混淆节点"
                type: ss
                server: o.example.com
                port: 8388
                cipher: aes-256-gcm
                password: pw
                plugin: obfs-local
                plugin-opts:
                  mode: http
                  host: obs.example.com
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].obfs).isEqualTo("obfs-local")
    }

    @Test
    fun `skip-cert-verify 映射为 insecure`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "insecure 节点"
                type: trojan
                server: i.example.com
                port: 443
                password: pw
                skip-cert-verify: true
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].insecure).isTrue()
    }

    @Test
    fun `servername 与 sni 都可取到 SNI`() {
        val a = ClashProxies.parse(
            """
            proxies:
              - {name: a, type: trojan, server: a.com, port: 443, password: p, servername: sa.com}
            """.trimIndent()
        )
        val b = ClashProxies.parse(
            """
            proxies:
              - {name: b, type: trojan, server: b.com, port: 443, password: p, sni: sb.com}
            """.trimIndent()
        )

        assertThat(a[0].sni).isEqualTo("sa.com")
        assertThat(b[0].sni).isEqualTo("sb.com")
    }

    // -----------------------------------------------------------------------
    // 跳过非节点条目（这是真实配置的常态）
    // -----------------------------------------------------------------------

    @Test
    fun `策略组条目被跳过而不是报错`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - name: "香港 01"
                type: ss
                server: hk1.example.com
                port: 8388
                cipher: aes-256-gcm
                password: pw
              - name: "自动选择"
                type: select
                proxies: ["香港 01"]
              - name: "故障转移"
                type: fallback
                proxies: ["香港 01"]
                url: http://www.gstatic.com/generate_204
              - name: "直连"
                type: direct
              - name: "拒绝"
                type: reject
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].name).isEqualTo("香港 01")
    }

    @Test
    fun `proxy-groups 里的 proxies 键不会误判为节点段`() {
        // 关键：段识别必须只看**零缩进**的顶层 `proxies:`。
        val nodes = ClashProxies.parse(
            """
            proxy-groups:
              - name: "代理"
                type: select
                proxies:
                  - DIRECT
                  - REJECT
            proxies:
              - {name: real, type: ss, server: r.com, port: 80, cipher: aes-128-gcm, password: p}
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].server).isEqualTo("r.com")
    }

    @Test
    fun `整条 flow 形式的节点也能解析`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - {name: "流式", type: vless, server: f.com, port: 443, uuid: b831381d-6324-4d53-ad4f-8cda48b30811, network: ws, tls: true}
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].protocolRaw).isEqualTo("vless")
    }

    // -----------------------------------------------------------------------
    // 边界与容错
    // -----------------------------------------------------------------------

    @Test
    fun `没有 proxies 段时返回空列表而不是抛异常`() {
        val nodes = ClashProxies.parse(
            """
            port: 7890
            socks-port: 7891
            mode: rule
            """.trimIndent()
        )

        assertThat(nodes).isEmpty()
    }

    @Test
    fun `完整配置里只取 proxies 段`() {
        val nodes = ClashProxies.parse(
            """
            mixed-port: 7890
            mode: rule
            proxies:
              - {name: n1, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}
              - {name: n2, type: ss, server: b.com, port: 81, cipher: aes-128-gcm, password: p}
            proxy-groups:
              - name: g1
                type: select
                proxies: [n1, n2]
            rules:
              - MATCH,DIRECT
            """.trimIndent()
        )

        assertThat(nodes).hasSize(2)
    }

    @Test
    fun `端口非数字的条目被跳过而非抛异常`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - {name: bad, type: ss, server: b.com, port: "abc", cipher: aes-128-gcm, password: p}
              - {name: ok, type: ss, server: c.com, port: 80, cipher: aes-128-gcm, password: p}
            """.trimIndent()
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].name).isEqualTo("ok")
    }

    @Test
    fun `缺 server 或 type 的条目被跳过`() {
        val nodes = ClashProxies.parse(
            """
            proxies:
              - {name: no-server, type: ss, port: 80, cipher: aes-128-gcm, password: p}
              - {name: no-type, server: a.com, port: 80}
              - {name: unknown-type, type: some-new-proto, server: a.com, port: 80}
            """.trimIndent()
        )

        assertThat(nodes).isEmpty()
    }

    @Test
    fun `注释与空行被忽略`() {
        val nodes = ClashProxies.parse(
            """
            # 我的配置
            proxies:

              - {name: n, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p} # 行尾注释

              - {name: m, type: ss, server: b.com, port: 81, cipher: aes-128-gcm, password: "p#1"}
            """.trimIndent()
        )

        assertThat(nodes).hasSize(2)
        // 引号内的 # 不是注释
        assertThat(nodes[1].password).isEqualTo("p#1")
    }

    @Test
    fun `产出的节点能通过 validate 且能被映射层消费`() {
        // 端到端：Clash 解析 → ShareNode → toProxyNode，与分享链接路径汇合。
        val nodes = ClashProxies.parse(
            """
            proxies:
              - {name: e2e, type: vmess, server: e.com, port: 443, uuid: b831381d-6324-4d53-ad4f-8cda48b30811, alterId: 0, network: ws}
            """.trimIndent()
        )

        val sn = nodes.single()
        sn.validate() // 不抛即通过
        val p = sn.toProxyNode("id-e2e")
        assertThat(p.server).isEqualTo("e.com")
        assertThat(p.rawJson).isEmpty()
        assertThat(p.credentialRef).isNull()
    }

    @Test
    fun `CRLF 换行同样可解析`() {
        // Windows 用户粘贴的配置普遍是 CRLF；按 \n split 会留下行尾 \r。
        val nodes = ClashProxies.parse(
            "proxies:\r\n  - {name: crlf, type: ss, server: a.com, port: 80, cipher: aes-128-gcm, password: p}\r\n"
        )

        assertThat(nodes).hasSize(1)
        assertThat(nodes[0].server).isEqualTo("a.com")
    }
}
