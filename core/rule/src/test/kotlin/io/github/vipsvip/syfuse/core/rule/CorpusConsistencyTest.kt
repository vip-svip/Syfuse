package io.github.vipsvip.syfuse.core.rule

import com.google.common.truth.Truth.assertThat
import com.google.common.truth.Truth.assertWithMessage
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 规则一致性测试：把 tests/corpus 的混合语法输入喂给引擎，
 * 逐条比对 tests/expect.tsv 的期望决策与优先级。
 *
 * 这是"规则语义正确"的主要可验证证据。任何一行不通过都视为回归，
 * 不允许通过放宽期望值来"变绿"。
 */
class CorpusConsistencyTest {

    private val corpusDir: File = File(
        System.getProperty("syfuse.corpusDir")
            ?: error("缺少系统属性 syfuse.corpusDir；请通过 Gradle 的 test 任务运行本测试"),
    )

    private val expectFile: File = corpusDir.parentFile.resolve("expect.tsv")

    private fun loadReport(): RuleParser.Report {
        val corpusText = corpusDir.listFiles { f: File -> f.isFile && f.name.endsWith(".txt") }!!
            .sortedBy { it.name }
            .joinToString("\n") { it.readText() }
        return RuleParser.parse(corpusText, sourceId = "corpus")
    }

    @Test
    fun `语料逐条与期望表一致`() {
        assertThat(corpusDir.isDirectory).isTrue()
        assertThat(expectFile.isFile).isTrue()

        val report = loadReport()
        val engine = FilterEngine.compile(report.networkRules, report.diagnostics)

        val rows = expectFile.readLines()
            // 去 BOM 与 \r：编辑器可能给首行加上 UTF-8 BOM，若不剥离会让注释行判定失效
            .map { it.removePrefix("\uFEFF").trimEnd('\r') }
            .filter { it.isNotBlank() && !it.trimStart().startsWith("#") }
        assertThat(rows).isNotEmpty()

        var checked = 0
        for (row in rows) {
            val cols = row.split('\t')
            if (cols.size < 8) error("expect.tsv 列数不足 8：$row")

            val kind = cols[0].trim()
            val value = cols[1].trim()
            val resourceType = ResourceType.fromToken(cols[2].trim()) ?: ResourceType.OTHER
            val documentHost = cols[3].trim().takeIf { it != "-" }
            val method = cols[4].trim().takeIf { it != "-" }
            val wantDecision = cols[5].trim()
            val wantPriority = cols[6].trim()
            val note = cols[7].trim()

            val request = when (kind) {
                "host" -> MatchRequest(
                    host = value,
                    resourceType = resourceType,
                    documentHost = documentHost,
                    method = method,
                )
                "url" -> MatchRequest(
                    host = MatchRequest.extractHost(value),
                    url = value,
                    resourceType = resourceType,
                    documentHost = documentHost,
                    method = method,
                )
                else -> error("expect.tsv 中出现未知 kind：$kind")
            }

            val result = engine.match(request, collectChain = true)

            assertWithMessage("决策不符 → 输入 `$value`（$note）")
                .that(result.decision.name.lowercase())
                .isEqualTo(wantDecision)

            assertWithMessage("优先级不符 → 输入 `$value`（$note）")
                .that(priorityName(result.priority))
                .isEqualTo(wantPriority)

            checked++
        }

        println("[CorpusConsistency] 通过 $checked 条期望；引擎规则数=${engine.ruleCount}，" +
            "索引化=${engine.indexedRuleCount}，线性=${engine.linearRuleCount}；" +
            "总行数=${report.totalLines}，忽略行数=${report.ignoredLines}，" +
            "元素规则=${report.cosmeticRules.size}，ERROR=${report.errors.size}，WARNING=${report.warnings.size}")
    }

    /**
     * 语料解析计数与 Go 侧逐项对齐。
     *
     * 这些常量是**两侧共同的金标准**：Go 侧
     * `syfuse/rule/corpus_test.go` 的 `TestCorpus_NoRuleSilentlyDropped`
     * 断言同一组值。任何一侧的解析行为漂移，这里或那里就会红。
     *
     * 之所以要单独立一条：上面那条"逐条期望"只覆盖被查询的域名，
     * 若某个规则被静默丢弃、且没有任何期望行命中它，逐条断言仍然全绿。
     */
    @Test
    fun `语料解析计数与 Go 侧一致`() {
        val report = loadReport()
        assertWithMessage("网络规则数").that(report.networkRules).hasSize(29)
        assertWithMessage("元素规则数").that(report.cosmeticRules).hasSize(6)
        assertWithMessage("忽略行数").that(report.ignoredLines).isEqualTo(35)
        assertWithMessage("总行数").that(report.totalLines).isEqualTo(71)
        assertWithMessage("ERROR 数").that(report.errors).hasSize(1)
        assertWithMessage("WARNING 数").that(report.warnings).hasSize(1)
    }

    @Test
    fun `非法规则产出带行号的错误诊断`() {
        val report = loadReport()
        assertThat(report.errors).hasSize(1)

        val error = report.errors.single()
        assertWithMessage("错误诊断必须带行号，且行号应指向非法规则所在行")
            .that(error.lineNo)
            .isGreaterThan(0)
        assertThat(error.raw).contains("||^")
        assertThat(error.severity).isEqualTo(RuleDiagnostic.Severity.ERROR)
    }

    @Test
    fun `未识别修饰符产出警告而不是静默丢弃`() {
        val report = loadReport()
        val warned = report.warnings.map { it.raw }
        assertThat(warned).contains("||warn.test^\$notamodifier")
    }

    @Test
    fun `badfilter 生效并留下审计记录`() {
        val report = loadReport()
        val engine = FilterEngine.compile(report.networkRules, report.diagnostics)
        val audit = engine.diagnostics.filter { it.message.contains("\$badfilter") }
        assertThat(audit).isNotEmpty()

        // 同形规则被关闭后，bad.test 必须回到默认放行
        val result = engine.match(MatchRequest(host = "bad.test"))
        assertThat(result.decision).isEqualTo(Decision.NONE)
    }

    @Test
    fun `注释与列表头被忽略而不产生规则`() {
        val report = loadReport()
        // 语料含 3 行纯注释/列表头 + 以 # 开头的注释行
        assertThat(report.ignoredLines).isAtLeast(4)
        assertThat(report.networkRules.none { it.raw.startsWith("[") }).isTrue()
    }

    private fun priorityName(p: Priority): String = when (p) {
        Priority.IMPORTANT_ALLOW -> "important-allow"
        Priority.IMPORTANT_BLOCK -> "important-block"
        Priority.NORMAL_ALLOW -> "normal-allow"
        Priority.NORMAL_BLOCK -> "normal-block"
        Priority.NONE -> "none"
    }
}
