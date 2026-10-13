#!/usr/bin/env bash
# 单元测试：JVM 侧全部可测逻辑。
#
# 覆盖范围（都是**纯 JVM**、不依赖 Android 设备，因此在任何 CI runner 上都能真跑）：
#   :core:rule   规则解析 / 索引匹配 / 优先级模型 / corpus 一致性
#   :domain      设置目录、配置编解码、引擎状态机、流量计量
#   :data        实体映射、凭据保险库格式（Robolectric 之外的纯逻辑部分）
#
# 退出码：0 全绿；1 有失败；2 环境缺失。绝不把环境缺失当成功。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

syfuse_info "JDK: $JAVA_HOME"
syfuse_info "Gradle: $SYFUSE_GRADLE"
syfuse_info "Android SDK: ${ANDROID_HOME:-<未设置>}"

TASKS=(
  ":core:rule:test"
  ":domain:test"
)

# :data 是 Android library；其单元测试任务存在但需要 SDK。有 SDK 才带上。
if [ -n "$ANDROID_HOME" ]; then
  TASKS+=(":data:testDebugUnitTest")
  # :ui 的玻璃策略层是纯 Kotlin 逻辑（不碰 Android 运行时），可在 JVM 上直接验证：
  # 强度档位、白名单、层数上限、P90 超标判定、超标自动降档状态机。
  # 注意：这里验证的是**策略逻辑**，不是"模糊是否真的生效"——后者需要真机，
  # 由 tests/ui_glass_perf.sh 负责（无设备时 SKIP）。
  TASKS+=(":ui:testDebugUnitTest")
fi

echo
syfuse_info "执行: ${TASKS[*]}"
echo "-------------------------------------------------------------------"
syfuse_gradle "${TASKS[@]}" --console=plain --rerun-tasks
status=$?
echo "-------------------------------------------------------------------"

# 汇总各模块的 JUnit XML 结果——只报"结论"是不够的，这里给出可核对的计数。
python_exe=""
for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
  if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then python_exe="$c"; break; fi
done

if [ -n "$python_exe" ]; then
  "$python_exe" - "$SYFUSE_ROOT" <<'PY'
import glob, os, sys, xml.etree.ElementTree as ET
root = sys.argv[1]
files = glob.glob(os.path.join(root, "*", "build", "test-results", "**", "TEST-*.xml"), recursive=True)
tests = failures = errors = skipped = 0
for f in files:
    try:
        r = ET.parse(f).getroot()
    except Exception:
        continue
    tests += int(r.get("tests", 0)); failures += int(r.get("failures", 0))
    errors += int(r.get("errors", 0)); skipped += int(r.get("skipped", 0))
print(f"[UNIT SUMMARY] 套件文件={len(files)} 用例={tests} 失败={failures} 错误={errors} 跳过={skipped}")
if files and failures == 0 and errors == 0:
    print("[UNIT RESULT] PASS")
elif not files:
    print("[UNIT RESULT] NO-RESULTS（没有找到测试报告，不能判定为通过）")
else:
    print("[UNIT RESULT] FAIL")
PY
fi

exit $status
