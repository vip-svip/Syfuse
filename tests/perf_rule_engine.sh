#!/usr/bin/env bash
# 规则引擎性能基准：产出**真实测量值**，写入 tests/perf_results.txt。
#
# 与 run_unit.sh 的区别：run_unit 只关心"对不对"，本脚本关心"多快"。
# 之所以分开，是因为性能测量需要大堆（默认 3g）和较长耗时，不适合塞进常规回归。
#
# 用法：
#   tests/perf_rule_engine.sh                  # 默认 100 万条规则 × 5 轮（另有 1 轮预热不计入）
#   tests/perf_rule_engine.sh 200000           # 自定义规模
#   tests/perf_rule_engine.sh 1000000 7        # 自定义规模与测量轮数
#
# 多轮取中位数 + 丢弃预热轮，是提示词第 11 部分"多轮取中位数"的落地方式，
# 不是可选项。实测：单轮基准测的**永远是暖机阶段** —— 同一机器同一代码
# 单轮得到 0.9651ms / 1.3183ms（相差 36%），而 5 轮里的第 1 轮 p99=1.3077ms、
# 第 3~5 轮稳定在 0.69ms 左右。拿单轮数字跟预算比，比的是 JIT 收敛过程。
#
# 注意：本脚本**不做阈值断言**。理由：机器差异会让固定阈值变成噪音，
# 一个跨机器都成立的阈值实际上不存在，硬断言只会让 CI 变红变绿都不可信。
# 它负责把原始数字与**明确判定**落到文件里，由人对照 README 的性能预算判读（R4：不编造性能结论）。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

RULES="${1:-1000000}"
ROUNDS="${2:-5}"
OUT="$SYFUSE_ROOT/tests/perf_results.txt"

syfuse_info "规模: $RULES 条规则 × $ROUNDS 轮（+1 轮预热不计入）"
syfuse_info "输出: $OUT"
syfuse_info "机器信息:"
{
  echo "    os       : $(uname -srm 2>/dev/null || echo unknown)"
  echo "    java     : $("$JAVA_HOME/bin/java" -version 2>&1 | head -1)"
  echo "    cpu      : $(nproc 2>/dev/null || echo '?') 逻辑核"
} 
echo

echo "=== Syfuse 规则引擎基准 $(date -u +%Y-%m-%dT%H:%M:%SZ) ===" >> "$OUT"
echo "主机: $(uname -srm 2>/dev/null)  JVM: $("$JAVA_HOME/bin/java" -version 2>&1 | head -1)" >> "$OUT"

syfuse_gradle :core:rule:test \
  --tests 'io.github.vipsvip.syfuse.core.rule.RuleEngineBenchmark' \
  -Psyfuse.runPerf=true \
  -Psyfuse.perfRuleCount="$RULES" \
  -Psyfuse.perfRounds="$ROUNDS" \
  -Psyfuse.perfWarmupRounds=1 \
  --rerun-tasks --console=plain
status=$?

echo
if [ "$status" -eq 0 ]; then
  syfuse_info "基准完成。最新记录："
  tail -n 16 "$OUT" | sed 's/^/  /'
else
  echo "基准执行失败（exit=$status）" >&2
fi
exit $status
