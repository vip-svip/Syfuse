#!/usr/bin/env bash
# verify_deliverable.sh —— 交付完整性校验（Linux / macOS / CI 版）
# 与 verify_deliverable.ps1 检查项一致（共 7 组）。
# 本文件必须是「无 BOM 的 UTF-8」——带 BOM 会破坏 shebang；这一点也由第 6 组检查。
#
# 用法:
#   bash tools/verify_deliverable.sh                        # 自动找当前目录最大的 .md
#   bash tools/verify_deliverable.sh docs/prompt.md 1000    # 指定文件与行数下限
# 退出码: 0 = 全部通过；1 = 存在失败项（CI 直接当门禁）

set -u

FILE="${1:-}"
MIN_LINES="${2:-1}"

fail=0
warn=0

c_ok()   { printf '  [PASS] %s\n' "$1"; }
c_bad()  { printf '  [FAIL] %s\n' "$1"; fail=$((fail+1)); }
c_warn() { printf '  [WARN] %s\n' "$1"; warn=$((warn+1)); }
head2()  { printf '\n== %s ==\n' "$1"; }

# 按字节安全截断：先 head -c 再 iconv -c 丢掉被切断的半个多字节字符。
# （直接 cut -c 在 C locale 下按字节切，中文会被切成乱码尾）
if command -v iconv >/dev/null 2>&1; then
  clip() { head -c "${1:-64}" | iconv -f UTF-8 -t UTF-8 -c 2>/dev/null; }
else
  clip() { head -c "${1:-64}"; }
fi
# 去掉行内 `代码段`
strip_code() { sed -E 's/`[^`]*`//g'; }

# ---------- 0. 定位文件 ----------
if [ -z "$FILE" ]; then
  FILE="$(ls -S ./*.md 2>/dev/null | head -n 1)"
fi
if [ -z "$FILE" ] || [ ! -f "$FILE" ]; then
  printf '[FAIL] 找不到待校验文件（用法: %s <file> [min_lines]）\n' "$0"
  exit 1
fi

BYTES=$(wc -c < "$FILE" | tr -d ' ')
LINES=$(awk 'END{print NR}' "$FILE")

head2 "目标文件"
printf '  %s\n' "$FILE"
printf '  字节 %s   行数 %s\n' "$BYTES" "$LINES"

# ---------- 1. 体积与行数 ----------
head2 "1. 体积与行数（确认没被截断）"
if [ "$BYTES" -gt 0 ]; then c_ok "字节数 $BYTES"; else c_bad "文件为空"; fi
if [ "$LINES" -ge "$MIN_LINES" ]; then c_ok "行数 $LINES >= 下限 $MIN_LINES"; else c_bad "行数 $LINES < 下限 $MIN_LINES"; fi

FIRST=$(sed -n '1p' "$FILE")
LAST=$(sed -n "${LINES}p" "$FILE")
if [ -n "$(printf '%s' "$FIRST" | tr -d '[:space:]')" ]; then
  c_ok "首行非空：$(printf '%s' "$FIRST" | clip 48)"
else
  c_bad "首行为空（疑似截断）"
fi
if [ -n "$(printf '%s' "$LAST" | tr -d '[:space:]')" ]; then
  c_ok "末行非空：$(printf '%s' "$LAST" | clip 48)"
else
  c_bad "末行为空（疑似截断）"
fi

# ---------- 2. 结构完整性 ----------
head2 "2. 结构完整性"
H2=$(grep -c '^## ' "$FILE" || true)
H3=$(grep -c '^### ' "$FILE" || true)
FENCE=$(grep -c '^```' "$FILE" || true)
if [ "$H2" -gt 0 ]; then c_ok "## 章节 $H2 个"; else c_bad "没有任何 ## 章节"; fi
if [ "$H3" -gt 0 ]; then c_ok "### 小节 $H3 个"; else c_warn "没有 ### 小节"; fi
if [ $((FENCE % 2)) -eq 0 ]; then c_ok "代码围栏 $FENCE 个（成对）"; else c_bad "代码围栏 $FENCE 个（奇数 = 有未闭合代码块）"; fi

DUP=$(grep '^### ' "$FILE" | sort | uniq -d || true)
if [ -n "$DUP" ]; then
  while IFS= read -r d; do c_warn "小节标题重复：$d"; done < <(printf '%s\n' "$DUP")
else
  c_ok "无重复小节标题"
fi

# ---------- 3. 占位符 / 未完成标记 ----------
head2 "3. 占位符与未完成标记"
PH_PATTERN='TODO|FIXME|<your-name>|待补写|\[略\]|（略）|\.\.\.$|XXX'
META='❌|禁止|不许|不要|除外|占位符|核对|扫描|校验|检测'
# 围栏按长度配对：``` 开的块只能被 ``` 关，```` 开的块要被 ```` 关。
# 否则脚本源码里出现的 ``` 会翻转状态，使代码块内的 TODO 被误判为文档缺陷。
# 模式必须用 ENVIRON 传入：awk 的 -v 会处理转义序列，会把 \. 变成 .，
# 使 \.\.\.$ 退化成「任意三字符结尾」（= 匹配几乎每一行）。ENVIRON 不做转义处理。
PH=$(PH_PATTERN="$PH_PATTERN" META="$META" awk '
  BEGIN {
    pat = ENVIRON["PH_PATTERN"]; meta = ENVIRON["META"]
    fence = 0; flen = 0; inblock = 0
  }
  {
    if (match($0, /^`{3,}/)) {
      ticks = RLENGTH
      if (fence == 0) { fence = 1; flen = ticks } else if (ticks >= flen) { fence = 0; flen = 0 }
      next
    }
    if ($0 !~ pat) next
    if (fence == 1) { inblock++; next }
    line = $0
    gsub(/`[^`]*`/, "", line)
    if (line ~ pat && $0 !~ meta) print "FAIL\t" NR "\t" $0
    else print "WARN\t" NR "\t" $0
  }
  END { if (inblock > 0) print "BLOCK\t" inblock "\t" }
' "$FILE")
if [ -z "$PH" ]; then
  c_ok "正文无占位符 / 未完成标记"
else
  while IFS=$'\t' read -r kind a b; do
    case "$kind" in
      FAIL)  c_bad  "L${a} 正文残留占位符：$(printf '%s' "$b" | clip 60)" ;;
      WARN)  c_warn "L${a} 命中位于行内代码/规则说明，忽略：$(printf '%s' "$b" | clip 60)" ;;
      BLOCK) c_warn "代码块内含占位符类字样 ${a} 处（示例/源码，已跳过不计失败）" ;;
    esac
  done < <(printf '%s\n' "$PH")
fi

# ---------- 4. 密钥 / 令牌残留 ----------
head2 "4. 密钥与令牌残留扫描"
SEC_PATTERN='ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[A-Za-z0-9-]{10,}'
SEC=$(grep -nE "$SEC_PATTERN" "$FILE" || true)
if [ -z "$SEC" ]; then
  c_ok "未发现明文密钥 / 令牌"
else
  while IFS= read -r s; do c_bad "疑似密钥：$(printf '%s' "$s" | clip 60)"; done < <(printf '%s\n' "$SEC")
fi

# ---------- 5. 需求覆盖 ----------
head2 "5. 需求逐条覆盖"
REQS='1 SY-Fusion 作为参考基线|SY-Fusion
2 对标 Shadowrocket 且只能多不能少|只能多不能少
3 主界面非 Web 端|WebView / H5 / 混合框架承载主界面
4 支持 root 与 LSPosed|LSPosed
5 UI 合理化与深度定制|UI 深度定制
6 极客化与全功能自定义|全功能自定义化
7 交付为模块形态|模块层（systemless，交付主体）
8 模块内置软件并互相增强|内含 sidecar APK
9 新仓库与短英文模块名|syfuse
10 作者署名取 GitHub 账号|账号名运行时解析
11 GitHub 令牌能力与权限|已授权使用
12 反僵化（不许死脑筋）|反僵化条款
13 不设能力上限（完全体）|完全体授权
14 单文件自包含|单文件 · 自包含
15 Shadowrocket 有就不重复造|Shadowrocket 有就不加
16 功能必须真的能用|功能可用性门禁
17 节点与 IP 防泄漏|IP 防泄漏
18 AdGuard Home 功能并入|AdGuard Home
19 uBlock Origin Lite 功能并入|uBlock Origin Lite
20 SY/SY-Fusion 功能全量并入并有逐项映射|逐项复现映射
21 新流行配色|OKLCH
22 透明玻璃（受控）|透明玻璃实现规范
23 App 内浏览器面板开关|启用浏览器面板
24 交付完整性纪律|把没读完说成读过了
25 界面可重新设计（不必照搬实拍）|仅供参考，可重新设计
26 功能合理分区|UI 深度定制与功能分区
27 交付前必须跑校验门禁|verify_deliverable
28 玻璃性能预算|P90 ≤ 24ms
29 模块 action.sh 菜单|action.sh
30 Hook 内置能力|证书固定绕过
31 多布局预设 + 可自建 Tab|布局预设
32 SY/SY-Fusion 可复现规格|复现规格
33 完整界面方案主题系统（六维）|完整界面方案（皮肤）
34 自定义主题方案（逐角色编辑/取色/分区覆盖/主题包）|自定义主题方案
35 默认 7 Tab 布局|默认使用布局 B
36 作者 GitHub 地址内置到 App|作者与开源信息**内置到 App 里**
37 提速与并行执行规范|提速与并行执行规范
38 安全提速红线（提速不得降质）|安全提速红线'

TOTAL=0
MISS=0
while IFS='|' read -r NAME KW; do
  [ -z "$NAME" ] && continue
  TOTAL=$((TOTAL+1))
  if grep -qF -- "$KW" "$FILE"; then
    c_ok "$NAME"
  else
    c_bad "$NAME  —— 未找到关键词：$KW"
    MISS=$((MISS+1))
  fi
done <<EOF
$REQS
EOF
printf '  需求项 %s 条，缺失 %s 条\n' "$TOTAL" "$MISS"

# ---------- 6. 脚本自身编码（踩过的坑，必须成为检查项）----------
head2 "6. 脚本编码规范"
SCAN_DIR=$(dirname "$FILE")
FOUND=0
# 勘误 E1：原先 PRUNE 是一个未加引号的字符串，bash 会在交给 find 之前先做通配展开
# （仓库里存在 app/build/... 这类目录时会被展开成几十条路径），find 于是报
# "paths must precede expression" 并静默返回空 —— 第 6 组形同虚设。
# 改用 read -ra 切成数组（read 是内建命令，不做通配展开），每个 pattern 原样交给 find。
read -ra PRUNE <<<'-path */.git/* -o -path */build/* -o -path */node_modules/* -o -path */downloads/* -o -path */screenshots/* -o -path */logs/* -o -path */dist/* -o -path */.gradle/*'
while IFS= read -r s; do
  [ -z "$s" ] && continue
  FOUND=$((FOUND+1))
  bom=$(head -c 3 "$s" | od -An -tu1 | tr -s ' ' | sed 's/^ //')
  hasext_sh=0; case "$s" in *.sh) hasext_sh=1;; esac
  if [ "$bom" = "239 187 191" ]; then
    if [ "$hasext_sh" -eq 1 ]; then c_bad "$(basename "$s") 带 BOM，会破坏 shebang，必须去掉"
    else c_ok "$(basename "$s") 带 UTF-8 BOM（PS 5.1 解码正确）"; fi
  else
    if [ "$hasext_sh" -eq 1 ]; then c_ok "$(basename "$s") 无 BOM（shebang 正确）"
    else c_bad "$(basename "$s") 缺少 UTF-8 BOM：Windows PowerShell 5.1 会按 ANSI 解码中文并报语法错误"; fi
  fi
  # 换行符：.sh 混入 CRLF 会让 bash 报 "\r: command not found"（Windows 上极易发生）
  if [ "$hasext_sh" -eq 1 ]; then
    if grep -qU $'\r' "$s" 2>/dev/null; then
      c_bad "$(basename "$s") 含 CRLF 换行，bash 会报 '\r: command not found'，必须转成 LF"
    else
      c_ok "$(basename "$s") 换行为 LF（正确）"
    fi
  fi
done < <(find "$SCAN_DIR" -type f \( -name '*.ps1' -o -name '*.sh' \) ! \( "${PRUNE[@]}" \) 2>/dev/null)
# 勘误 E2：找不到脚本由 WARN 改成 FAIL —— 「静默跳过」正是 E1 能长期潜伏的原因。
[ "$FOUND" -eq 0 ] && c_bad "未找到 .ps1/.sh 可检查 —— 扫描根 $SCAN_DIR 下没有任何脚本，第 6 组形同虚设"

# ---------- 7. 附录与磁盘一致性（base64 模式）----------
head2 "7. 附录与磁盘一致性"
SYNC="$SCAN_DIR/sync_appendix.ps1"
if [ ! -f "$SYNC" ]; then
  c_warn "未找到 sync_appendix.ps1（跳过附录一致性检查）"
elif command -v powershell >/dev/null 2>&1; then
  CHK=$(powershell -NoProfile -ExecutionPolicy Bypass -File "$SYNC" -Mode Check 2>&1)
  if printf '%s' "$CHK" | grep -q '结论：一致'; then
    c_ok "附录与 tools/ 三个脚本一致（base64 逐字节）"
  else
    c_bad "附录与 tools/ 脚本不一致（用 sync_appendix.ps1 -Mode Embed 修复）"
    printf '%s\n' "$CHK" | grep '\[FAIL\]' | head -5 | while IFS= read -r l; do c_bad "    $l"; done
  fi
else
  c_warn "无 powershell，跳过附录一致性检查"
fi

# ---------- 汇总 ----------
head2 "汇总"
if [ "$fail" -eq 0 ]; then
  printf '  全部通过（WARN %s 项）\n' "$warn"
  exit 0
else
  printf '  失败 %s 项，WARN %s 项 —— 不允许交付\n' "$fail" "$warn"
  exit 1
fi
