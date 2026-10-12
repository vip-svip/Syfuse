#!/usr/bin/env bash
# 账号解析 + 包名/署名统一改写。
#
# 提示词要求：**账号名必须在运行时解析，绝不硬编码**。解析顺序：
#   1. $SYFUSE_OWNER（显式覆盖，CI 用）
#   2. $GITHUB_ACTOR / $GITHUB_REPOSITORY_OWNER（GitHub Actions 自动注入）
#   3. `gh api user --jq .login`（已登录的 gh CLI）
#   4. git remote origin URL 中的 owner
#   5. 保守默认值（见下），并**显式打印这是默认值而非解析结果**
#
# 规范化：GitHub 账号允许连字符，Java 包名不允许 -> 去掉连字符并转小写。
#   例：vip-svip -> vipsvip
#
# 用法：
#   tools/set_owner.sh --print      # 只打印解析结果，不改文件
#   tools/set_owner.sh --apply      # 解析并改写仓库
#   tools/set_owner.sh --apply --owner someone
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# 保守默认值：由用户提供的模块地址 https://github.com/vip-svip/SY-Fusion 推导。
# 这不是"解析结果"，是"没有账号可用时的可运行兜底"，必须能被一眼看出来。
FALLBACK_OWNER="vip-svip"

OLD_SEG="vipsvip"   # 仓库中当前使用的包名段（由旧解析结果产出）

resolve_owner() {
  local src owner

  if [ -n "${SYFUSE_OWNER:-}" ]; then
    owner="$SYFUSE_OWNER"; src="\$SYFUSE_OWNER"
  elif [ -n "${GITHUB_REPOSITORY_OWNER:-}" ]; then
    owner="$GITHUB_REPOSITORY_OWNER"; src="\$GITHUB_REPOSITORY_OWNER"
  elif [ -n "${GITHUB_ACTOR:-}" ]; then
    owner="$GITHUB_ACTOR"; src="\$GITHUB_ACTOR"
  elif command -v gh >/dev/null 2>&1; then
    owner="$(gh api user --jq .login 2>/dev/null || true)"
    src="gh api user"
    if [ -z "$owner" ]; then
      # gh 存在但未登录：继续往下走，不在此处报错
      owner=""
    fi
  else
    owner=""
  fi

  if [ -z "$owner" ]; then
    local url
    url="$(git config --get remote.origin.url 2>/dev/null || true)"
    if [ -n "$url" ]; then
      owner="$(printf '%s' "$url" | sed -E 's#(git@|https?://)[^/:]+[:/]##; s#/.*##; s#\.git$##')"
      src="git remote origin"
    fi
  fi

  if [ -z "$owner" ]; then
    owner="$FALLBACK_OWNER"; src="保守默认值（未解析到账号）"
  fi

  printf '%s\t%s\n' "$owner" "$src"
}

normalize() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d -- '-_.'; }

main() {
  local mode="print" explicit=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --print) mode="print"; shift;;
      --apply) mode="apply"; shift;;
      --owner) explicit="${2:-}"; shift 2;;
      *) echo "未知参数: $1" >&2; exit 2;;
    esac
  done

  local owner src
  if [ -n "$explicit" ]; then
    owner="$explicit"; src="--owner 显式指定"
  else
    IFS=$'\t' read -r owner src < <(resolve_owner)
  fi

  local seg; seg="$(normalize "$owner")"
  [ -n "$seg" ] || { echo "解析出的账号规范化后为空，拒绝继续" >&2; exit 2; }

  echo "账号解析来源 : $src"
  echo "账号原文     : $owner"
  echo "规范化包名段 : $seg"
  echo "应用包名     : io.github.$seg.syfuse"
  echo "Kotlin 包路径: io/github/$seg/syfuse"
  echo "署名         : $owner"
  echo

  if [ "$mode" = "print" ]; then
    echo "（--print 模式，未改动任何文件）"
    return 0
  fi

  if [ "$seg" = "$OLD_SEG" ]; then
    echo "包名段与当前一致（$OLD_SEG），仅同步署名，不移动目录。"
  fi

  # ---------- 1. 文本内容替换 ----------
  # 只替换我们自己的命名空间，避免误伤第三方包名（例如 com.github.topjohnwu.libsu）。
  echo "-- 改写包名引用 --"
  changed=0
  while IFS= read -r f; do
    if grep -q "io\.github\.$OLD_SEG\.syfuse\|io/github/$OLD_SEG/syfuse" "$f" 2>/dev/null; then
      sed -i.bak "s#io\.github\.$OLD_SEG\.syfuse#io.github.$seg.syfuse#g; s#io/github/$OLD_SEG/syfuse#io/github/$seg/syfuse#g" "$f"
      rm -f "$f.bak"
      changed=$((changed+1))
    fi
  done < <(git ls-files 2>/dev/null || find . -type f -not -path './.git/*' -not -path '*/build/*')
  echo "   改写文件数: $changed"

  # ---------- 2. 目录搬迁 ----------
  if [ "$seg" != "$OLD_SEG" ]; then
    echo "-- 搬迁源码目录 --"
    while IFS= read -r d; do
      newd="$(printf '%s' "$d" | sed "s#io/github/$OLD_SEG/syfuse#io/github/$seg/syfuse#")"
      if [ "$d" != "$newd" ] && [ -d "$d" ]; then
        mkdir -p "$(dirname "$newd")"
        mv "$d" "$newd"
        echo "   $d -> $newd"
      fi
    done < <(find . -type d -path "*io/github/$OLD_SEG/syfuse" -not -path './.git/*' -not -path '*/build/*')
    # 清理空的旧目录链
    find . -type d -empty -path "*io/github/$OLD_SEG*" -delete 2>/dev/null || true
    find . -type d -empty -path "*io/github*" -delete 2>/dev/null || true
  fi

  # ---------- 3. 署名同步 ----------
  echo "-- 同步署名 --"
  if [ -f module/module.prop ]; then
    sed -i.bak "s/^author=.*/author=$owner/" module/module.prop && rm -f module/module.prop.bak
    echo "   module/module.prop author=$owner"
  fi
  if [ -f NOTICE ]; then
    sed -i.bak -E "s/^Copyright \([Cc]\) .*/Copyright (C) $(date +%Y) $owner/" NOTICE && rm -f NOTICE.bak
    echo "   NOTICE 版权行已更新"
  fi
  if [ -f LICENSE ]; then
    sed -i.bak -E "s/^Copyright \([Cc]\) .*/Copyright (C) $(date +%Y) $owner/" LICENSE && rm -f LICENSE.bak
    echo "   LICENSE 版权行已更新"
  fi

  echo
  echo "完成。建议接着执行："
  echo "  tools/scan_secrets.py && tests/run_unit.sh && tests/run_module_checks.sh"
}

main "$@"
