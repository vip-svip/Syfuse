#!/usr/bin/env bash
# 单元测试：JVM 侧全部可测逻辑。
#
# 覆盖范围（都是**纯 JVM**、不依赖 Android 设备，因此在任何 CI runner 上都能真跑）：
#   :core:rule        规则解析 / 索引匹配 / 优先级模型 / corpus 一致性
#   :domain           设置目录、配置编解码、引擎状态机、流量计量
#   :daemon-protocol  跨语言协议契约（读 Go 源码核对）+ 与真实 syfused 进程的 UDS 往返
#   :data             实体映射、凭据保险库格式（Robolectric 之外的纯逻辑部分）
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
  # :daemon-protocol 是纯 JVM 模块（协议模型 + 客户端 + 传输接口），
  # 因此**不需要 Android SDK**就能跑。它的往返测试会用本机 Go 工具链
  # 真实编译一个 syfused 并跑通「客户端 → UDS → 守护进程」——
  # 这是 M3 在沙箱里能得到的最强证据（缺 Go 工具链时它**失败**而不是跳过）。
  ":daemon-protocol:test"
)

# 其余是 Android library；其单元测试任务存在但需要 SDK。有 SDK 才带上。
if [ -n "$ANDROID_HOME" ]; then
  TASKS+=(":data:testDebugUnitTest")
  # :ui 的玻璃策略层是纯 Kotlin 逻辑（不碰 Android 运行时），可在 JVM 上直接验证：
  # 强度档位、白名单、层数上限、P90 超标判定、超标自动降档状态机。
  # 注意：这里验证的是**策略逻辑**，不是"模糊是否真的生效"——后者需要真机，
  # 由 tests/ui_glass_perf.sh 负责（无设备时 SKIP）。
  TASKS+=(":ui:testDebugUnitTest")
  # :engine 的 UdsDataPlane 适配层：用替身传输验证"守护进程的错误 → App 的展示原因"
  # 这条映射（真机 LocalSocket 传输无法在 JVM 单测里跑，见 daemon-protocol 的说明）。
  TASKS+=(":engine:testDebugUnitTest")
fi

echo
syfuse_info "执行: ${TASKS[*]}"
echo "-------------------------------------------------------------------"

# 先清掉上一轮的测试报告。
# 不清理的后果是**静默的假绿**：下面的汇总按 XML 文件计数，而某个模块被移出
# TASKS 之后，它的旧报告还躺在磁盘上，于是汇总里仍然出现那个模块、
# 用例数还是上一轮的数字。实测踩过：`:daemon` 的协议契约测试迁到
# `:daemon-protocol` 之后，旧报告仍被计入，模块数虚报为 6 个。
cleaned=0
while IFS= read -r d; do
  [ -n "$d" ] && rm -rf "$d" && cleaned=$((cleaned + 1))
done < <(find "$SYFUSE_ROOT" -type d -name test-results -path '*/build/*' -prune -print 2>/dev/null)
syfuse_info "已清理上一轮测试报告目录: $cleaned 个"

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
# 必须用递归 glob：`core:rule` / `core:common` 的模块目录在 `core/` 下一层，
# 用 `*` 匹配不到，会让它们的用例**静默不计入汇总** ——
# 于是"失败=0"看起来是绿的，实际上根本没统计到那些模块。
files = sorted(set(glob.glob(os.path.join(root, "**", "build", "test-results", "**", "TEST-*.xml"), recursive=True)))
tests = failures = errors = skipped = 0
skipped_names = []
for f in files:
    try:
        r = ET.parse(f).getroot()
    except Exception:
        continue
    tests += int(r.get("tests", 0)); failures += int(r.get("failures", 0))
    errors += int(r.get("errors", 0)); skipped += int(r.get("skipped", 0))
    for tc in r.iter("testcase"):
        if list(tc.iter("skipped")):
            skipped_names.append(tc.get("classname", "") + "#" + (tc.get("name") or ""))
mods = sorted({os.path.relpath(f, root).split(os.sep + "build" + os.sep)[0] for f in files})
print(f"[UNIT SUMMARY] 模块={len(mods)} 套件文件={len(files)} 用例={tests} 失败={failures} 错误={errors} 跳过={skipped}")
print(f"[UNIT MODULES] {' '.join(mods) if mods else '<无>'}")
# 跳过项必须**点名**。只报一个数字会让"有 1 个跳过"变成没人追问的常态，
# 而跳过正是最容易藏住"其实没验证"的地方。
for n in sorted(set(skipped_names)):
    print(f"[UNIT SKIPPED] {n}")
if files and failures == 0 and errors == 0:
    print("[UNIT RESULT] PASS")
elif not files:
    print("[UNIT RESULT] NO-RESULTS（没有找到测试报告，不能判定为通过）")
else:
    print("[UNIT RESULT] FAIL")
PY
fi

exit $status
