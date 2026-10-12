#!/usr/bin/env bash
# UI 资源完整性检查（不需要设备、不需要编译）。
#
# 解决的问题：Compose 项目里最容易出现的低级失败是"引用了不存在的 @string/@drawable/@mipmap"，
# 这类问题在真机安装后表现为启动崩溃，排查成本高。这里在编译前就把引用全部解析一遍。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
cd "$SYFUSE_ROOT"

PASS=0; FAIL=0
pass() { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
fail() { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }

RES_DIRS=$(find app/src/main/res ui/src/main/res -maxdepth 1 -type d -name 'values*' 2>/dev/null | tr '\n' ' ')
echo "== 资源目录 =="
echo "  $RES_DIRS"

# ---------- 1. 收集已定义的资源名 ----------
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

collect() { # $1=tag -> 输出 name 列表
  grep -rhoE "<$1[^>]*name=\"[^\"]+\"" $RES_DIRS 2>/dev/null \
    | sed -E "s/.*name=\"([^\"]+)\".*/\1/" | sort -u > "$tmp/$1.txt"
}
for tag in string drawable color mipmap style bool integer; do collect "$tag"; done

# 文件型资源（drawable/xxx.png、mipmap-anydpi-v26/xxx.xml）不以 <tag name=> 形式出现，
# 必须单独索引，否则 "@drawable/ic_launcher_foreground" 这类合法引用会被误报为未命中。
index_file_resources() { # $1=type  $2=目录 glob
  local type="$1"; shift
  for d in $@; do
    [ -d "$d" ] || continue
    find "$d" -maxdepth 1 -type f 2>/dev/null \
      | sed -E 's#.*/##; s#\.(xml|png|jpg|jpeg|webp|gif|svg)$##' >> "$tmp/$type.txt"
  done
  sort -u -o "$tmp/$type.txt" "$tmp/$type.txt" 2>/dev/null || true
}
index_file_resources drawable \
  app/src/main/res/drawable app/src/main/res/drawable-* \
  ui/src/main/res/drawable ui/src/main/res/drawable-*
index_file_resources mipmap \
  app/src/main/res/mipmap app/src/main/res/mipmap-* \
  ui/src/main/res/mipmap ui/src/main/res/mipmap-*

# ---------- 2. 收集全部引用 ----------
# XML 与 Manifest 里的 @type/name
grep -rhoE '@(string|drawable|mipmap|color|style|bool|integer)/[A-Za-z0-9_.]+' \
  app/src/main ui/src/main 2>/dev/null | sed 's/^@//' | sort -u > "$tmp/refs.txt"
# Kotlin 里的 R.type.name
grep -rhoE '\bR\.(string|drawable|mipmap|color|style|bool|integer)\.[A-Za-z0-9_]+' \
  app/src ui/src 2>/dev/null | sed -E 's/^R\.//' | sort -u >> "$tmp/refs.txt"
sort -u -o "$tmp/refs.txt" "$tmp/refs.txt"

echo
echo "== 引用解析 =="
missing=0
while IFS= read -r ref; do
  [ -n "$ref" ] || continue
  type="${ref%%/*}"; name="${ref#*/}"
  # android 平台资源（android: 前缀已在 grep 阶段排除，此处兜底）
  case "$name" in android:*) continue;; esac
  if [ -f "$tmp/$type.txt" ] && grep -qxF "$name" "$tmp/$type.txt"; then
    :
  else
    # 资源可能定义在依赖库里（例如 compose material 的 color），无法在此静态判定；
    # 只有当我们自己的 values 里连同名条目都没有时才算可疑，但仍标为 WARN 而非 FAIL。
    printf '  [WARN] 引用未在本仓库 values 中找到（可能来自依赖库）: @%s\n' "$ref"
    missing=$((missing+1))
  fi
done < "$tmp/refs.txt"

total=$(wc -l < "$tmp/refs.txt" | tr -d ' ')
echo "  共解析引用 $total 条，未在本仓库命中 $missing 条"
[ "$missing" -eq 0 ] && pass "全部资源引用均在本仓库有定义" \
  || printf '  [INFO] 未命中项已逐条列出，需人工确认是否来自依赖库\n'

# ---------- 3. 自适应图标三件套 ----------
echo
echo "== 启动图标 =="
[ -f app/src/main/res/mipmap-anydpi-v26/ic_launcher.xml ] && pass "mipmap-anydpi-v26/ic_launcher.xml 存在" || fail "缺自适应图标描述文件"
[ -f app/src/main/res/drawable/ic_launcher_foreground.xml ] && pass "前景层存在" || fail "缺 ic_launcher_foreground"
if grep -q 'ic_launcher_foreground' app/src/main/res/mipmap-anydpi-v26/ic_launcher.xml 2>/dev/null; then
  pass "自适应图标引用了前景层"
else
  fail "自适应图标未引用前景层"
fi

# ---------- 4. XML 良构性 ----------
echo
echo "== XML 良构性 =="
py=""
for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
  if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then py="$c"; break; fi
done
if [ -n "$py" ]; then
  bad=$("$py" - "$SYFUSE_ROOT" <<'PY'
import os, sys, xml.etree.ElementTree as ET
root = sys.argv[1]; bad = []
for base in ("app/src/main", "ui/src/main"):
    for dirpath, _, files in os.walk(os.path.join(root, base)):
        for f in files:
            if f.endswith(".xml"):
                p = os.path.join(dirpath, f)
                try: ET.parse(p)
                except Exception as e: bad.append(f"{p}: {e}")
print("\n".join(bad))
PY
)
  if [ -z "$bad" ]; then
    pass "全部 XML 良构"
  else
    fail "XML 解析失败："; echo "$bad"
  fi
else
  printf '  [WARN] 无 Python，跳过 XML 良构检查\n'
fi

# ---------- 5. 主题与语言资源一致性 ----------
echo
echo "== 文案资源 =="
if [ -f app/src/main/res/values/strings.xml ]; then
  n=$(grep -cE '<string ' app/src/main/res/values/strings.xml)
  pass "strings.xml 定义 $n 条文案"
fi
[ -f app/src/main/res/values/themes.xml ] && pass "themes.xml 存在" || fail "缺 themes.xml"
[ -f app/src/main/res/values/colors.xml ] && pass "colors.xml 存在" || fail "缺 colors.xml"

# ---------- 6. 内联文案计数（如实统计，不粉饰） ----------
# 骨架阶段界面文案直接内联在 Compose 里，尚未抽取到 strings.xml。
# 这是**已知缺口**，因此这里只统计并打印，不判定失败——但也不隐瞒。
echo
echo "== 内联中文文案统计 =="
inline_total=0
while IFS= read -r f; do
  c=$(grep -oE '"[^"]*[一-龥][^"]*"' "$f" 2>/dev/null | grep -vE '^\s*//' | wc -l | tr -d ' ')
  if [ "$c" -gt 0 ]; then
    printf '  %-58s %s 处\n' "${f#*/main/kotlin/io/github/vipsvip/syfuse/}" "$c"
    inline_total=$((inline_total + c))
  fi
done < <(find ui/src/main app/src/main -name '*.kt' -type f 2>/dev/null)
echo "  合计内联文案: $inline_total 处"
if [ "$inline_total" -gt 0 ]; then
  printf '  [PEND] 界面文案尚未抽取到 strings.xml（%s 处）。见 CHANGELOG「已知缺口」\n' "$inline_total"
else
  pass "界面文案已全部走资源文件"
fi

printf '\n===========================================\n'
printf 'ui-assets 汇总: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
printf '===========================================\n'
[ "$FAIL" -eq 0 ] || exit 1
exit 0
