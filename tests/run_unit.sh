#!/usr/bin/env bash
#
# 单元测试。覆盖：错误模型、配置编译、设置过滤、特权探测。
#
# 提示词第 12 部分要求本脚本在 CI 运行且失败阻断合并。
# 设计与 run_e2e.sh 的差别：本脚本只跑纯逻辑（JVM 单测），不起隧道、不需真机，
# 因此能在任何环境运行，是最快的反馈回路。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "==> Syfuse 单元测试"
echo "    工作目录: ${PROJECT_ROOT}"

# Gradle 不可用时给出明确指引而非静默通过 —— 静默通过会掩盖真实失败
if ! command -v ./gradlew >/dev/null 2>&1 && [ ! -f "./gradlew" ]; then
    echo "错误：未找到 gradlew。请先执行 gradle wrapper 或安装 Gradle。" >&2
    exit 1
fi

# 只跑 JVM 单测（testDebugUnitTest），不跑仪器测试。
# --continue 让多个模块的失败一次性暴露，而不是在第一个失败处停下，
# 否则修复-重跑会反复多轮，拖慢反馈。
./gradlew :core:testDebugUnitTest \
          :domain:testDebugUnitTest \
          :engine:testDebugUnitTest \
          :privileged:testDebugUnitTest \
          :ui:testDebugUnitTest \
          --continue

echo "==> 单元测试通过"
