#!/usr/bin/env bash
#
# UI 截图与可视化回归（提示词第 12 部分）。
#
# 定位：「截图 + 布局、裁剪、性能断言（可视化回归）」。
#
# 当前状态（M1）：**未实现，以明确失败退出**。
# 原因：截图需要真机或 Paparazzi 一类的 JVM 截图库，且需要 JDK。
#
# 提示词 16.7 与 16.8 要求 README 内的截图必须是「真机截图或动图，
# 不用设计稿冒充」。因此本脚本的产出将来直接服务于 README 的可信度。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

cat >&2 <<'EOF'
==> UI 截图与可视化回归：尚未实现

原因：需要 Android SDK + JDK（真机仪器测试或 Paparazzi）。

待补齐（CI 或本地有 SDK 的环境）：
  1. 四个 Tab 各截一张（浅色 / 深色 / AMOLED 三种主题）
  2. 空状态与错误态各截一张
  3. 断言：无文字截断（200% 字体缩放）、无布局裁剪、触控目标 >= 48dp
  4. 输出到 tests/screenshots/，供 README 引用

退出码 78（EX_CONFIG）表示「环境未配置」。
EOF

exit 78
