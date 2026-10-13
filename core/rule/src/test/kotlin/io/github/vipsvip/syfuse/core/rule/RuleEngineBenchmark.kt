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
 * 规模由 `-Dsyfuse.perfRuleCount` 控制，默认 100 万条（对应第 11 部分预算）；
 * 预热轮数由 `-Dsyfuse.perfWarmupRounds` 控制（默认 1，**不计入结论**）；
 * 测量轮数由 `-Dsyfuse.perfRounds` 控制，默认 5 轮。
 *
 * ## 为什么必须多轮 + 丢弃预热轮
 *
 * 提示词第 11 部分原文：
 *
 * > 所有指标必须由仓库内脚本产出并输出原始日志（**多轮取中位数**）。
 *
 * 这不是形式要求。本基准此前**只跑一轮**，同一台机器、同一份代码连测两次得到
 * p99 = 0.9651ms 与 1.3183ms（相差 36%），据此先后写下过"达标，余量 3.5%"
 * 和"超出预算 1.9 倍"两个互相矛盾的结论 —— 两个都是错的。
 *
 * 改成多轮后真相立刻显现（5 轮实测）：
 *
 * | 轮次 | p99 | 该轮总耗时 |
 * |---|---|---|
 * | 1 | 1.3077ms | 76278ms |
 * | 2 | 0.7922ms | 67216ms |
 * | 3 | 0.6962ms | 60016ms |
 * | 4 | 0.6878ms | 60177ms |
 * | 5 | 0.6960ms | 60366ms |
 *
 * 第 1 轮比稳态慢 **88%**：它测的是 JIT 编译与 GC 收敛的过程，不是稳态性能。
 * 单轮基准**永远**测在暖机阶段上 —— 所以那个数字既不代表稳态，也不能拿来跟预算比。
 *
 * 因此本基准现在：预热轮跑完整采样但丢弃 → 测量轮各跑一轮 → 对逐轮 p99 取中位数，
 * 并打印最小/最大/离散，让人能直接看到稳定性。
 *
 * ## 判读
 *
 * 采样中有一半是"未命中域名 Trie"的请求。它们与命中的请求一样，
 * 都要逐条扫描线性规则列表（见 `FilterEngine.collectCandidates`）——
 * 因此 p50 就已经不低（约 0.60ms），p99 与 p50 的差距主要来自 GC 与调度抖动。
 * 详见 README「性能预算与实测（第 11 部分）」。
 */
@EnabledIfSystemProperty(named = "syfuse.runPerf", matches = "true")
class RuleEngineBenchmark {

    private val ruleCount: Int =
        System.getProperty("syfuse.perfRuleCount")?.toIntOrNull() ?: 1_000_000

    /** 测量轮数。规范要求"多轮取中位数"，因此这里必须 > 1。 */
    private val rounds: Int =
        (System.getProperty("syfuse.perfRounds")?.toIntOrNull() ?: 5).coerceAtLeast(1)

    /**
     * **不计入结论**的预热轮数，默认 1。
     *
     * 实测数据说明为什么必须有它：5 轮测量得到 p99 = 1.3077 / 0.7922 / 0.6962 / 0.6878 / 0.6960 ms，
     * 第 1 轮比第 3~5 轮慢 **88%**，每轮总耗时 76278ms vs 60016ms。
     * 也就是说第 1 轮测的是 JIT 编译与 GC 收敛的过程，不是稳态性能。
     *
     * 这解释了此前为什么"同一台机器同一份代码"两次单轮测量会得到
     * 0.9651ms 与 1.3183ms —— 单轮基准测的**始终是暖机阶段**，
     * 那个数字既不代表稳态，也不能拿来跟预算比。
     */
    private val warmupRounds: Int =
        (System.getProperty("syfuse.perfWarmupRounds")?.toIntOrNull() ?: 1).coerceAtLeast(0)

    /** 每轮采样次数。规范要求"≥10 万次采样"，所以默认值不能低于 100_000。 */
    private val samples: Int =
        (System.getProperty("syfuse.perfSamples")?.toIntOrNull() ?: 100_000).coerceAtLeast(1)

    /** 预热迭代数。JIT 收敛远不需要跑满一轮采样，跑满只是浪费几倍时间。 */
    private val warmupIterations = 20_000

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

    /** 单轮统计结果（单位 ms）。 */
    private data class RoundStats(
        val round: Int,
        val p50: Double,
        val p90: Double,
        val p99: Double,
        val max: Double,
        val totalMs: Long,
    )

    @Test
    fun `百万级规则编译与匹配基准`() {
        println("[Benchmark] 目标规则数=$ruleCount 预热轮=$warmupRounds 测量轮=$rounds 每轮采样=$samples")
        assertThat(rounds).isGreaterThan(1)

        val text = generate(ruleCount)

        var report: RuleParser.Report? = null
        val parseMs = measureTimeMillis { report = RuleParser.parse(text, "benchmark") }
        val parsed = report!!
        println(
            "[Benchmark] 解析耗时=${parseMs}ms，网络规则=${parsed.networkRules.size}，" +
                "错误=${parsed.errors.size}，警告=${parsed.warnings.size}",
        )
        assertThat(parsed.errors).isEmpty()

        var engine: FilterEngine? = null
        val compileMs = measureTimeMillis {
            engine = FilterEngine.compile(parsed.networkRules, parsed.diagnostics)
        }
        val eng = engine!!
        println(
            "[Benchmark] 编译耗时=${compileMs}ms，索引化=${eng.indexedRuleCount}，" +
                "线性=${eng.linearRuleCount}",
        )

        // 采样：命中与未命中各半，覆盖 Trie 命中路径与 Trie 命中后仍需扫描线性列表的路径
        val hosts = Array(samples) { i ->
            if (i % 2 == 0) "ads${i % ruleCount}.example${i % 251}.test" else "nomatch$i.unknown$i.test"
        }

        // 小规模预热：让 JIT 先把热路径编译掉，避免第一轮全部时间花在解释执行上。
        repeat(2) { for (i in 0 until minOf(warmupIterations, samples)) eng.match(MatchRequest(host = hosts[i])) }

        // 一轮完整采样。抽成函数是为了让"预热轮"与"测量轮"用完全相同的代码路径 ——
        // 如果预热轮走的是另一条实现，它就不构成有效的预热。
        fun runRound(label: String, round: Int, total: Int): RoundStats {
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

            val stats = RoundStats(
                round = round,
                p50 = percentile(0.50),
                p90 = percentile(0.90),
                p99 = percentile(0.99),
                max = latencies.last() / 1_000_000.0,
                totalMs = totalMs,
            )
            println(
                "[Benchmark] $label $round/$total: p50=%.4fms p90=%.4fms p99=%.4fms max=%.4fms 总耗时=${stats.totalMs}ms"
                    .format(stats.p50, stats.p90, stats.p99, stats.max),
            )
            return stats
        }

        // 预热轮：跑完整一轮采样但**不计入结论**。见 warmupRounds 的说明。
        val warmups = ArrayList<RoundStats>(warmupRounds)
        repeat(warmupRounds) { r -> warmups += runRound("预热轮（不计入）", r + 1, warmupRounds) }

        val all = ArrayList<RoundStats>(rounds)
        repeat(rounds) { r -> all += runRound("测量轮", r + 1, rounds) }

        val p50s = all.map { it.p50 }
        val p90s = all.map { it.p90 }
        val p99s = all.map { it.p99 }
        val medP50 = median(p50s)
        val medP90 = median(p90s)
        val medP99 = median(p99s)
        val totalMs = all.sumOf { it.totalMs }
        val dispersion = (p99s.max() - p99s.min()) / medP99 * 100.0

        // 注意：字符串拼接会让 `.format(...)` 只作用于最后一个字面量（Kotlin 里
        // `+` 的优先级高于成员调用），结果就是前半段的 %.4f 原样打印出来。
        // 所以先把整行拼成一个值，再统一 format。
        val medianLine = "[Benchmark] 中位数: p50=%.4fms p90=%.4fms p99=%.4fms（p99 最小 %.4f / 最大 %.4f，离散 %.1f%%）"
            .format(medP50, medP90, medP99, p99s.min(), p99s.max(), dispersion)

        println("[Benchmark] p99 逐轮: ${p99s.joinToString(" ") { "%.4f".format(it) }}ms")
        if (warmups.isNotEmpty()) {
            println("[Benchmark] p99 预热轮（已丢弃）: ${warmups.joinToString(" ") { "%.4f".format(it.p99) }}ms")
        }
        println(medianLine)
        println("[Benchmark] 吞吐=%.0f 次/秒（按总耗时）".format(samples * rounds * 1000.0 / totalMs))

        // 预算取自提示词第 11 部分：单次规则匹配 p99 < 1ms。
        // **刻意不可配置**：把预算做成命令行参数，等于给"调预算让它通过"留后门，
        // 而本文件此前正是被一个自造的 0.5ms 预算坑过一次。
        val verdict = if (medP99 < BUDGET_P99_MS) {
            "达标（余量 %.1f%%）".format((BUDGET_P99_MS - medP99) / BUDGET_P99_MS * 100.0)
        } else {
            "**未达标**（超出 %.1f%%）".format((medP99 - BUDGET_P99_MS) / BUDGET_P99_MS * 100.0)
        }
        println("[Benchmark] 预算 p99 < ${BUDGET_P99_MS}ms → 中位数 %.4fms %s".format(medP99, verdict))

        // 记录到仓库文件，作为第 11 部分性能预算的原始证据
        val out = java.io.File(
            System.getProperty("syfuse.perfOutput")
                ?: error("缺少系统属性 syfuse.perfOutput"),
        )
        out.parentFile?.mkdirs()
        out.appendText(
            buildString {
                appendLine("=== Syfuse 规则引擎基准 ===")
                appendLine("规则数: $ruleCount  预热轮: $warmupRounds  测量轮: $rounds  每轮采样: $samples")
                appendLine("解析耗时: ${parseMs}ms")
                appendLine("编译耗时: ${compileMs}ms")
                appendLine("索引化规则: ${eng.indexedRuleCount}  线性规则: ${eng.linearRuleCount}")
                appendLine("总采样: ${samples * rounds}  总耗时: ${totalMs}ms")
                if (warmups.isNotEmpty()) {
                    appendLine("p99 预热轮（已丢弃）: ${warmups.joinToString(" ") { "%.4f".format(it.p99) }}ms")
                }
                appendLine("p99 逐轮: ${p99s.joinToString(" ") { "%.4f".format(it) }}ms")
                appendLine(
                    "中位数: p50=%.4fms p90=%.4fms p99=%.4fms（最小 %.4f / 最大 %.4f，离散 %.1f%%）"
                        .format(medP50, medP90, medP99, p99s.min(), p99s.max(), dispersion),
                )
                appendLine("吞吐: %.0f 次/秒".format(samples * rounds * 1000.0 / totalMs))
                appendLine("判定（预算 p99 < ${BUDGET_P99_MS}ms）: $verdict")
                appendLine()
            },
        )
    }

    private companion object {
        /**
         * 提示词第 11 部分：`单次规则匹配 p99 | < 1ms | 基准脚本（≥10 万次采样）`。
         *
         * 不可通过命令行覆盖 —— 见 `verdict` 处的说明。
         */
        const val BUDGET_P99_MS = 1.0

        fun median(xs: List<Double>): Double {
            val s = xs.sorted()
            return if (s.size % 2 == 1) {
                s[s.size / 2]
            } else {
                (s[s.size / 2 - 1] + s[s.size / 2]) / 2.0
            }
        }
    }
}
