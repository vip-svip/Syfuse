package io.github.vipsvip.syfuse.domain.engine

import com.google.common.truth.Truth.assertThat
import com.google.common.truth.Truth.assertWithMessage
import io.github.vipsvip.syfuse.core.rule.Decision
import io.github.vipsvip.syfuse.core.rule.FilterEngine
import io.github.vipsvip.syfuse.core.rule.MatchRequest
import io.github.vipsvip.syfuse.core.rule.ResourceType
import io.github.vipsvip.syfuse.core.rule.RuleParser
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 跨语言一致性：把 `tests/expect.tsv` 的**每一行**喂给 [RuleRouter]，断言映射结果。
 *
 * 这是防"两套真相漂移"的关键一环，链条必须讲清楚：
 *
 *  1. `tests/corpus` + `tests/expect.tsv` 是 Kotlin 与 Go **共用的预言机**。
 *     Kotlin 侧 `core:rule` 的 CorpusConsistencyTest 与 Go 侧
 *     `syfuse/rule/corpus_test.go` 的 TestCorpusMatchesExpectations 各自消费它，
 *     证明两个语言的**引擎决策**在这些行上一致；
 *  2. 本类证明 [RuleRouter] 的**映射层**（引擎决策 → 出站动作）与同一张表一致：
 *     block→REJECT / allow→DIRECT / rewrite→DIRECT(+dnsRewrite) / none→默认动作。
 *
 * 两者合起来才构成完整证据：单有 1 只能证明引擎没漂移，单有 2 只能证明映射层
 * 自洽。本类红 = 映射层破坏；两侧引擎测试任一红 = 引擎漂移。
 *
 * 与语料文件的关系：本类只**读** `tests/corpus` 与 `tests/expect.tsv`，
 * 且**不硬编码任何条数** —— 这两个文件由 T6 单元维护（38 → ≥120 条扩容中），
 * 扩容后本类自动覆盖新行。唯一耦合是 8 列制表符格式与 decision 词表，
 * 这正是跨语言契约本身。
 *
 * method 列不参与：连接层拿不到 HTTP method（见 RoutingTarget 文档），
 * 语料中该列当前也全是 `-`。若将来语料启用 method 断言，说明 HTTP 层
 * （M8）已能提供该方法，届时在 RoutingTarget 补字段即可。
 */
class CorpusRouteMappingTest {

    /** expect.tsv 的一行（已剥离 BOM/CR、去掉注释与空行）。 */
    private data class Row(
        val kind: String,
        val value: String,
        val resourceType: ResourceType,
        val documentHost: String?,
        val wantDecision: String,
        val wantPriority: String,
        val note: String,
    )

    private val repoRoot: File = locateRepoRoot()

    private val engine: FilterEngine = compileCorpus()

    private val rows: List<Row> = loadExpectations()

    @Test
    fun `全部语料行按映射表给出出站动作（默认直连）`() {
        assertThat(rows).isNotEmpty()
        val router = RuleRouter(engine, RoutingPolicy(DefaultAction.DIRECT))

        for (row in rows) {
            val decision = router.decide(targetOf(row))

            assertWithMessage("出站动作不符 → 输入 `${row.value}`（${row.note}）")
                .that(decision.action)
                .isEqualTo(expectedAction(row.wantDecision, DefaultAction.DIRECT))
            // 映射层必须原样透传引擎决策，不得自行改判
            assertWithMessage("引擎决策未透传 → 输入 `${row.value}`（${row.note}）")
                .that(decision.engineDecision.name.lowercase())
                .isEqualTo(row.wantDecision)
            assertWithMessage("理由为空 → 输入 `${row.value}`（${row.note}）")
                .that(decision.reason)
                .isNotEmpty()
        }
        println("[CorpusRouteMapping] 默认直连策略：通过 ${rows.size} 行")
    }

    @Test
    fun `未命中行在默认代理策略下走代理`() {
        val router = RuleRouter(engine, RoutingPolicy(DefaultAction.PROXY("预言机默认节点")))

        var noneRows = 0
        for (row in rows) {
            val decision = router.decide(targetOf(row))
            val expected = expectedAction(row.wantDecision, DefaultAction.PROXY("预言机默认节点"))

            assertWithMessage("出站动作不符 → 输入 `${row.value}`（${row.note}）")
                .that(decision.action)
                .isEqualTo(expected)

            if (row.wantDecision == "none") {
                noneRows++
                // 未命中走默认代理时，节点名必须出现在决策与理由里
                assertThat(decision.node).isEqualTo("预言机默认节点")
                assertThat(decision.reason).contains("预言机默认节点")
            }
        }
        assertWithMessage("语料应至少含一条未命中行，否则默认动作分支形同虚设")
            .that(noneRows)
            .isGreaterThan(0)
        println("[CorpusRouteMapping] 默认代理策略：通过 ${rows.size} 行（其中未命中 $noneRows 行走代理）")
    }

    @Test
    fun `命中行透传规则与优先级且重写行载荷可见`() {
        val router = RuleRouter(engine, RoutingPolicy(DefaultAction.DIRECT))

        var rewriteRows = 0
        for (row in rows) {
            if (row.wantDecision == "none") continue
            val decision = router.decide(targetOf(row))

            // 命中的行必须带得出规则原文与优先级词表名（与 expect.tsv 同一套词）
            assertWithMessage("未透传命中规则 → 输入 `${row.value}`（${row.note}）")
                .that(decision.matchedRule)
                .isNotNull()
            assertWithMessage("优先级词表不符 → 输入 `${row.value}`（${row.note}）")
                .that(decision.reason)
                .contains(row.wantPriority)

            if (row.wantDecision == "rewrite") {
                rewriteRows++
                // 重写行的出站是直连（连接不出网），但载荷必须对上层可见
                assertWithMessage("重写行应为直连 → `${row.value}`（${row.note}）")
                    .that(decision.action)
                    .isEqualTo(RouteAction.DIRECT)
                assertWithMessage("重写载荷缺失 → `${row.value}`（${row.note}）")
                    .that(decision.dnsRewrite)
                    .isNotNull()
            } else {
                assertWithMessage("非重写行不应携带重写载荷 → `${row.value}`（${row.note}）")
                    .that(decision.dnsRewrite)
                    .isNull()
            }
        }
        assertWithMessage("语料应至少含一条重写行，否则 REWRITE 映射形同虚设")
            .that(rewriteRows)
            .isGreaterThan(0)
        println("[CorpusRouteMapping] 重写行数=$rewriteRows，载荷均可见")
    }

    // ---------------------------------------------------------------- 内部

    /**
     * expect.tsv 的 decision 列 → 出站动作。这是本类断言的映射表本体，
     * 与 [RuleRouter] 的 KDoc 逐行对应；两处必须同步修改。
     */
    private fun expectedAction(wantDecision: String, noMatch: DefaultAction): RouteAction =
        when (wantDecision) {
            "block" -> RouteAction.REJECT
            "allow" -> RouteAction.DIRECT
            "rewrite" -> RouteAction.DIRECT
            "none" -> when (noMatch) {
                DefaultAction.DIRECT -> RouteAction.DIRECT
                is DefaultAction.PROXY -> RouteAction.PROXY
            }
            else -> error("expect.tsv 出现未知 decision：$wantDecision")
        }

    private fun targetOf(row: Row): RoutingTarget = when (row.kind) {
        "host" -> RoutingTarget(
            host = row.value,
            resourceType = row.resourceType,
            documentHost = row.documentHost,
        )
        "url" -> RoutingTarget(
            host = MatchRequest.extractHost(row.value),
            url = row.value,
            resourceType = row.resourceType,
            documentHost = row.documentHost,
        )
        else -> error("expect.tsv 出现未知 kind：${row.kind}")
    }

    /** 与 Kotlin/Go 两侧语料加载器同一约定：按文件名排序、以换行拼接。 */
    private fun compileCorpus(): FilterEngine {
        val corpusDir = File(repoRoot, "tests/corpus")
        val text = corpusDir.listFiles { f: File -> f.isFile && f.name.endsWith(".txt") }!!
            .sortedBy { it.name }
            .joinToString("\n") { it.readText(Charsets.UTF_8) }
        val report = RuleParser.parse(text, sourceId = "corpus")
        // 语料**有意**包含非法行（产出带行号 ERROR）与未知修饰符（产出 WARNING），
        // 这里不断言零诊断 —— 那是 core:rule 的 CorpusConsistencyTest 的职责。
        return FilterEngine.compile(report.networkRules, report.diagnostics)
    }

    /** 期望表解析与 CorpusConsistencyTest / Go loadExpectations 同一约定。 */
    private fun loadExpectations(): List<Row> {
        val expectFile = File(repoRoot, "tests/expect.tsv")
        return expectFile.readLines(Charsets.UTF_8)
            // 去 BOM 与 \r：编辑器可能给首行加上 UTF-8 BOM，不剥离会让注释行判定失效
            .map { it.removePrefix("\uFEFF").trimEnd('\r') }
            .filter { it.isNotBlank() && !it.trimStart().startsWith("#") }
            .map { row ->
                val cols = row.split('\t')
                if (cols.size < 8) error("expect.tsv 列数不足 8：$row")
                Row(
                    kind = cols[0].trim(),
                    value = cols[1].trim(),
                    resourceType = ResourceType.fromToken(cols[2].trim()) ?: ResourceType.OTHER,
                    documentHost = cols[3].trim().takeIf { it != "-" },
                    wantDecision = cols[5].trim(),
                    wantPriority = cols[6].trim(),
                    note = cols[7].trim(),
                )
            }
    }

    /**
     * 从测试 JVM 工作目录向上找仓库根（判据同 Go 侧 findRepoRoot）。
     * 找不到就**失败**，绝不静默跳过 —— 静默跳过的语料测试会让
     * "映射层与预言机一致"这句话变成空话。
     */
    private fun locateRepoRoot(): File {
        var dir = File(System.getProperty("user.dir") ?: ".").absoluteFile
        while (true) {
            if (
                File(dir, "settings.gradle.kts").isFile &&
                File(dir, "tests/expect.tsv").isFile
            ) {
                return dir
            }
            dir = dir.parentFile
                ?: error("向上未找到仓库根（需要同时存在 settings.gradle.kts 与 tests/expect.tsv）")
        }
    }
}
