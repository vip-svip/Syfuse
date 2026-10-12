#!/usr/bin/env bash
#
# 规则一致性语料测试（提示词 6.1 与第 12 部分）。
#
# 定位：多语法混合输入 + 期望命中表（tests/corpus/ + expect.tsv）逐条断言。
#
# 当前状态（M1）：**未实现，以明确失败退出**。
# 原因：过滤规则引擎属差异增强层，计划于 M9 实现（提示词第 13 部分）。
#       M1 已建好语料目录骨架与期望表格式，届时直接填入即可。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

CORPUS_DIR="${SCRIPT_DIR}/corpus"
EXPECT_FILE="${CORPUS_DIR}/expect.tsv"

cat >&2 <<'EOF'
==> 规则一致性语料测试：尚未实现

原因：过滤引擎计划于 M9 实现；M1 只交付语料骨架。

已就绪：
  - tests/corpus/ 目录
  - tests/corpus/expect.tsv 的列格式已定义（见该文件表头）

M9 的填充计划：
  1. 按语法类别拆语料文件：network / modifiers / resource-types / cosmetic / hosts
  2. 逐条在 expect.tsv 写入期望命中结果
  3. 引擎实现后本脚本逐行比对，任一不符即失败

退出码 78（EX_CONFIG）表示「未配置完成」。
EOF

if [ -f "${EXPECT_FILE}" ]; then
    echo "（已检测到期望表骨架：${EXPECT_FILE}）" >&2
fi

exit 78
