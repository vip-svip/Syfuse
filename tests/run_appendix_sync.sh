#!/usr/bin/env bash
# 附录 ↔ tools/ 逐字节一致性（门禁第 7 组的补偿控制）
#
# 为什么需要这个脚本：
#   交付门禁的第 7 组要做同样的事，但它依赖 tools/sync_appendix.ps1 —— 那是随母版
#   分发的工具，不在附录 A/B/C 三件套里。本仓库没有它，第 7 组只报 WARN 后跳过。
#   于是「提示词附录被改过、而 tools/ 没跟着改」这类漂移不会被自动拦住。
#   M3 轮实测踩到过：附录 A 比磁盘多 12 行（含 PowerShell 7 的 -AsByteStream 分支），
#   附录 B 多 2 行（两条新需求），而门禁一直是绿的。
#   所以把「逐字节比对 + 已登记偏差清单」固化成这个脚本，退出码进 CI。
#
# 判定方式（不硬编码哈希，避免"改一次附录就要改一次脚本"）：
#   1. 从提示词附录还原三个脚本到 tests/_artifacts/appendix/
#   2. 对 run_agent.ps1：必须与 tools/ 逐字节相同（本项目未对它出勘误）
#   3. 对 verify_deliverable.ps1 / .sh：与 tools/ 的 unified diff 必须**逐字等于**
#      docs/errata/ 里已登记的偏差清单。清单变了 = 有一侧变了 = 必须重新审计。
#
# 退出码：0 通过；1 有失败；2 缺工具（python）；3 SKIP（找不到提示词文件）
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PASS=0; FAIL=0
pass() { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
fail() { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }
note() { printf '  [INFO] %s\n' "$1"; }
head2() { printf '\n== %s ==\n' "$1"; }

PY=""
for c in python3 python; do
  if command -v "$c" >/dev/null 2>&1; then PY="$c"; break; fi
done
[ -n "$PY" ] || { printf 'ERROR: 找不到 python（本检查需要它做 base64 解码）\n' >&2; exit 2; }

# 提示词文件位置：显式环境变量优先，其次本机已知路径。
PROMPT="${SYFUSE_PROMPT:-F:/Ai/Syfuse-完美提示词.md}"
if [ ! -f "$PROMPT" ]; then
  printf 'SKIP: 找不到提示词文件：%s\n' "$PROMPT" >&2
  printf '      补法：把提示词放到该路径，或设 SYFUSE_PROMPT=<路径>\n' >&2
  printf '      之后运行：bash tests/run_appendix_sync.sh（期望 退出码 0）\n' >&2
  exit 3
fi

ART="$ROOT/tests/_artifacts/appendix"
rm -rf "$ART"
mkdir -p "$ART"

# 成功时清掉还原产物：它们落在仓库内，会被交付门禁第 6 组当成"待检查的脚本"再扫一遍
# （等于把每个脚本查两遍，还会让计数虚高）。失败时保留，方便人直接看 diff。
cleanup() {
  if [ "$FAIL" -eq 0 ]; then
    rm -rf "$ART"
  else
    printf '  还原产物保留在：%s\n' "$ART"
  fi
}
trap cleanup EXIT

head2 "附录还原"
note "提示词：$PROMPT"
note "字节 $(wc -c < "$PROMPT" | tr -d ' ') / 行 $(awk 'END{print NR}' "$PROMPT")"
if ! "$PY" tools/extract_appendix.py "$PROMPT" "$ART"; then
  fail "附录还原失败（见上面的 ERROR）"
  printf '\n===========================================\n'
  printf 'appendix-sync 汇总: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
  printf '===========================================\n'
  exit 1
fi
pass "附录 A/B/C 全部按声明的字节数还原"

sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }
# stat 的参数在 GNU / BSD 上不一致，用 wc -c 保证可移植
fsize() { wc -c < "$1" | tr -d ' '; }

# 未出勘误的脚本：必须逐字节相同
head2 "未出勘误的脚本（必须逐字节相同）"
for f in run_agent.ps1; do
  if cmp -s "$ART/$f" "tools/$f"; then
    pass "$f 与附录逐字节相同（$(fsize "tools/$f") 字节，sha256 $(sha "tools/$f" | cut -c1-16)…）"
  else
    fail "$f 与附录不一致（附录 $(fsize "$ART/$f") 字节 / 磁盘 $(fsize "tools/$f") 字节）—— 用附录覆盖并复核"
  fi
done

# 已出勘误的脚本：偏差必须与登记的清单完全一致
head2 "已出勘误的脚本（偏差须等于 docs/errata/ 里的登记清单）"
check_errata() {
  local f="$1" manifest="$2" label="$3"
  local tmp="$ART/$f.diff"
  diff -u --label "$label" --label "tools/$f" "$ART/$f" "tools/$f" > "$tmp" 2>/dev/null || true
  if [ ! -f "$manifest" ]; then
    fail "$f 缺少登记清单 $manifest"
    return
  fi
  if cmp -s "$tmp" "$manifest"; then
    pass "$f 与附录的偏差 == 已登记勘误（$(grep -cE '^[+-][^+-]' "$manifest") 行内容差异，清单 $manifest）"
  else
    fail "$f 与附录的偏差**不等于**已登记勘误 —— 附录或 tools/ 有一侧被改过，必须重新审计"
    printf '      实际偏差（前 30 行）：\n'
    head -30 "$tmp" | sed 's/^/        /'
    printf '      若差异是本轮有意的，请重新同步、更新 %s，并在 docs/EVIDENCE.md 记录\n' "$manifest"
  fi
}

check_errata verify_deliverable.ps1 "$ROOT/docs/errata/appendix-A-verify_deliverable.ps1.diff" "附录A/verify_deliverable.ps1"
check_errata verify_deliverable.sh  "$ROOT/docs/errata/appendix-B-verify_deliverable.sh.diff"  "附录B/verify_deliverable.sh"

# 编码要求（附录自己声明的：A/C 带 BOM，B 无 BOM + LF）
head2 "编码要求（附录声明的）"
for f in verify_deliverable.ps1 run_agent.ps1; do
  b=$(head -c 3 "tools/$f" | od -An -tu1 | tr -s ' ' | sed 's/^ //')
  [ "$b" = "239 187 191" ] && pass "$f 带 UTF-8 BOM" || fail "$f 缺 UTF-8 BOM（PS 5.1 会按 ANSI 解码中文）"
done
b=$(head -c 3 "tools/verify_deliverable.sh" | od -An -tu1 | tr -s ' ' | sed 's/^ //')
[ "$b" = "239 187 191" ] && fail "verify_deliverable.sh 带 BOM，会破坏 shebang" || pass "verify_deliverable.sh 无 BOM（shebang 正确）"
if grep -qU $'\r' "tools/verify_deliverable.sh" 2>/dev/null; then
  fail "verify_deliverable.sh 含 CRLF，bash 会报 '\\r: command not found'"
else
  pass "verify_deliverable.sh 换行为 LF"
fi

printf '\n===========================================\n'
printf 'appendix-sync 汇总: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
printf '注意：本脚本只覆盖附录 A/B/C 三件套；tools/ 里其余脚本由门禁第 6 组管。\n'
printf '===========================================\n'
[ "$FAIL" -eq 0 ] || exit 1
exit 0
