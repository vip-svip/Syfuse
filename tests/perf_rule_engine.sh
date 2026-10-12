#!/usr/bin/env bash
# 规则引擎性能基准：产出**真实测量值**，写入 tests/perf_results.txt。
#
# 与 run_unit.sh 的区别：run_unit 只关心"对不对"，本脚本关心"多快"。
# 之所以分开，是因为性能测量需要大堆（默认 3g）和较长耗时，不适合塞进常规回归。
#
# 用法：
#   tests/perf_rule_engine.sh                  # 默认 100 万条规则
#   tests/perf_rule_engine.sh 200000           # 自定义规模
#
# 注意：本脚本**不做阈值断言**。理由：机器差异会让固定阈值变成噪音。
# 它只负责把原始数字落到文件里，由人对照 README 的性能预算判读（R4：不编造性能结论）。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

RULES="${1:-1000000}"
OUT="$SYFUSE_ROOT/tests/perf_results.txt"

syfuse_info "规模: $RULES 条规则"
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
  --rerun-tasks --console=plain
status=$?

echo
if [ "$status" -eq 0 ]; then
  syfuse_info "基准完成。最新记录："
  tail -n 12 "$OUT" | sed 's/^/  /'
else
  echo "基准执行失败（exit=$status）" >&2
fi
exit $status
