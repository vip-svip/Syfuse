#!/usr/bin/env bash
#
# UI 运行时逻辑检查（提示词第 12 部分）。
#
# 定位：「真实执行 UI 逻辑（不是静态检查），模拟导航与交互」。
#
# 当前状态（M1）：**未实现，以明确失败退出**。
# 原因：Compose UI 测试需要 Android 运行时（Robolectric 或真机），
# 本机无 Android SDK 与 JDK，无法执行。
#
# 这与 run_e2e.sh 的处理原则一致：不产出虚假的「通过」。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

cat >&2 <<'EOF'
==> UI 运行时逻辑检查：尚未实现

原因：需要 Android SDK + JDK 才能运行 Compose UI 测试（Robolectric 或仪器测试）。
      当前环境不具备，故不产生「通过」结论。

M1 已具备的前提：
  - SettingsFilterTest（纯 JVM 单测）已覆盖设置搜索与专家模式的过滤逻辑
  - ui_assets_check.py 已覆盖资产基线（静态）

待补齐（需 CI 的 Linux runner）：
  1. Robolectric 下运行 Compose 测试，模拟四 Tab 切换与返回栈
  2. 断言四个 Tab 均可到达且渲染预期标题
  3. 断言危险操作的二次确认弹窗可触发

退出码 78（EX_CONFIG）表示「环境未配置」，与「测试失败」（1）区分。
EOF

exit 78
