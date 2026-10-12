package io.github.vipsvip.syfuse.core.rule

import com.google.common.truth.Truth.assertThat
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.condition.EnabledIfSystemProperty
import kotlin.system.measureTimeMillis

/**
 * 规则引擎性能基准。
 *
 * 默认不参与常规测试运行（避免拖慢 CI），仅在显式传入
 * `-Dsyfuse.runPerf=true` 时执行。这样可以：
 *  ① 常规回归保持秒级；
 *  ② 性能数字只在被要求时产出，且产出的是**真实测量值**而不是估计值。
 *
 * 规模由 `-Dsyfuse.perfRuleCount` 控制，默认 100 万条（对应第 11 部分预算）。
 */
@EnabledIfSystemProperty(named = "syfuse.runPerf", matches = "true")
class RuleEngineBenchmark {

    private val ruleCount: Int =
        System.getProperty("syfuse.perfRuleCount")?.toIntOrNull() ?: 1_000_000

    /** 生成规则集：以域名类规则为主（这是真实规则集的主导形态），掺入少量正则与网段。 */
    private fun generate(count: Int): String {
        val sb = StringBuilder(count * 28)
        for (i in 0 until count) {
            val a = i % 251
            val b = (i / 251) % 251
            val c = i / (251 * 251)
            when (i % 100) {
                0 -> sb.append("||sub$i.ads$c.example$b.test^\n")
                1 -> sb.append("/tracker$i\\d+/\n")
                2 -> sb.append("10.$a.$b.0/24\n")
                else -> sb.append("||ads$i.example$a.test^\n")
            }
        }
        return sb.toString()
    }

    @Test
    fun `百万级规则编译与匹配基准`() {
        println("[Benchmark] 目标规则数=$ruleCount")

        val text = generate(ruleCount)

        var report: RuleParser.Report? = null
        val parseMs = measureTimeMillis { report = RuleParser.parse(text, "benchmark") }
        val parsed = report!!
        println("[Benchmark] 解析耗时=${parseMs}ms，网络规则=${parsed.networkRules.size}，" +
            "错误=${parsed.errors.size}，警告=${parsed.warnings.size}")
        assertThat(parsed.errors).isEmpty()

        var engine: FilterEngine? = null
        val compileMs = measureTimeMillis {
            engine = FilterEngine.compile(parsed.networkRules, parsed.diagnostics)
        }
        val eng = engine!!
        println("[Benchmark] 编译耗时=${compileMs}ms，索引化=${eng.indexedRuleCount}，" +
            "线性=${eng.linearRuleCount}")

        // 采样匹配：命中与未命中各半，覆盖 Trie 命中路径与 Trie 未命中后的线性回退路径
        val samples = 100_000
        val hosts = Array(samples) { i ->
            if (i % 2 == 0) "ads${i % ruleCount}.example${i % 251}.test" else "nomatch$i.unknown$i.test"
        }
        // 预热，避免把 JIT 冷启动算进 p99
        repeat(3) { for (h in hosts) eng.match(MatchRequest(host = h)) }

        val latencies = LongArray(samples)
        val totalMs = measureTimeMillis {
            for (i in 0 until samples) {
                val start = System.nanoTime()
                eng.match(MatchRequest(host = hosts[i]))
                latencies[i] = System.nanoTime() - start
            }
        }
        latencies.sort()

        fun percentile(p: Double): Double = latencies[((samples - 1) * p).toInt()] / 1_000_000.0

        println("[Benchmark] 匹配采样=$samples 次，总耗时=${totalMs}ms")
        println("[Benchmark] 单次匹配 p50=%.4fms  p90=%.4fms  p99=%.4fms  max=%.4fms"
            .format(percentile(0.50), percentile(0.90), percentile(0.99), latencies.last() / 1_000_000.0))
        println("[Benchmark] 吞吐=%.0f 次/秒".format(samples * 1000.0 / totalMs))

        // 记录到仓库文件，作为第 11 部分性能预算的原始证据
        val out = java.io.File(
            System.getProperty("syfuse.perfOutput")
                ?: error("缺少系统属性 syfuse.perfOutput"),
        )
        out.parentFile?.mkdirs()
        out.appendText(
            buildString {
                appendLine("=== Syfuse 规则引擎基准 ===")
                appendLine("规则数: $ruleCount")
                appendLine("解析耗时: ${parseMs}ms")
                appendLine("编译耗时: ${compileMs}ms")
                appendLine("索引化规则: ${eng.indexedRuleCount}  线性规则: ${eng.linearRuleCount}")
                appendLine("匹配采样: $samples  总耗时: ${totalMs}ms")
                appendLine("p50: %.4fms  p90: %.4fms  p99: %.4fms  max: %.4fms"
                    .format(percentile(0.50), percentile(0.90), percentile(0.99), latencies.last() / 1_000_000.0))
                appendLine("吞吐: %.0f 次/秒".format(samples * 1000.0 / totalMs))
                appendLine()
            },
        )
    }
}
