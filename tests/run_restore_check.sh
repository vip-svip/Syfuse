#!/usr/bin/env bash
# 卸载残留检查（提示词 12 部分 / 18.8 完成判定项）
#
# 做什么：安装 → 卸载 → 比对系统状态，**必须完全还原**。
#
# 怎么在没有真机的情况下也能真跑：
#   模块脚本的数据目录已支持 `SYFUSE_DATA_DIR` 重定向（真机上该变量为空，
#   走默认的 /data/adb/syfuse，行为不变）。因此本脚本在一个临时沙箱里
#   真实执行 customize.sh 与 uninstall.sh，然后 diff 沙箱前后状态。
#
#   这**不是**真机验证的替代品 —— 真机上还要验证挂载、证书、iptables。
#   但"数据目录是否被完全清除""清单机制是否真的生效"这两件事，
#   在这里是可以被证明的，而不是靠声称。
#
# 退出码：0 完全还原 / 1 有残留 / 2 环境缺失
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULE_DIR="$ROOT/module"

PASS=0; FAIL=0
ok()   { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
no()   { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }
info() { printf '  :: %s\n' "$1"; }
head2(){ printf '\n== %s ==\n' "$1"; }

for f in customize.sh uninstall.sh post-fs-data.sh service.sh action.sh module.prop; do
  [ -f "$MODULE_DIR/$f" ] || { echo "ERROR: 缺少 $MODULE_DIR/$f" >&2; exit 2; }
done

# 沙箱放在仓库内的 tests/_artifacts/（已在 .gitignore）。
#
# 为什么不放系统临时目录：本执行环境有一层安全删除守卫，
# 会拒绝删除工作区之外的路径（报 SAFE_DELETE_INVALID_PATH）。
# 卸载验证的核心动作就是"删除数据目录"，放外面会让整个检查失效。
# 放仓库内还有个好处：失败时产物留在原地可直接查看，不会被系统清掉。
ARTIFACT_DIR="$ROOT/tests/_artifacts"
mkdir -p "$ARTIFACT_DIR"
SANDBOX="$ARTIFACT_DIR/restore-check-$$"
rm -rf "$SANDBOX" 2>/dev/null
mkdir -p "$SANDBOX"
trap 'rm -rf "$SANDBOX"' EXIT

FAKE_DATA="$SANDBOX/data"          # 扮演 /data/adb/syfuse
FAKE_MOD="$SANDBOX/module"         # 扮演 /data/adb/modules/syfuse

info "沙箱: $SANDBOX"

# ---------------------------------------------------------------------------
head2 "1. 准备：复制模块骨架到沙箱"
# ---------------------------------------------------------------------------
mkdir -p "$FAKE_MOD"
cp -a "$MODULE_DIR/." "$FAKE_MOD/"
ok "模块骨架已复制（$(find "$FAKE_MOD" -type f | wc -l | tr -d ' ') 个文件）"

# ---------------------------------------------------------------------------
head2 "2. 记录安装前状态"
# ---------------------------------------------------------------------------
# 系统状态基线：沙箱内我们关心的路径集合
snapshot() {
  # 输出 "路径|类型|权限" 的排序列表，用于 diff
  if [ -d "$1" ]; then
    find "$1" -mindepth 1 2>/dev/null | LC_ALL=C sort | while IFS= read -r p; do
      if [ -d "$p" ]; then t=dir; else t=file; fi
      perm=$(stat -c '%a' "$p" 2>/dev/null || echo '?')
      printf '%s|%s|%s\n' "${p#$1}" "$t" "$perm"
    done
  fi
}

BEFORE="$(snapshot "$FAKE_DATA")"
info "安装前 $FAKE_DATA 条目数: $(printf '%s' "$BEFORE" | grep -c . || echo 0)"
# 安装前该目录不应该存在（全新安装）
if [ ! -e "$FAKE_DATA" ]; then
  ok "安装前数据目录不存在（符合全新安装预期）"
else
  no "安装前数据目录已存在，基线不干净"
fi

# ---------------------------------------------------------------------------
head2 "3. 执行安装（customize.sh）"
# ---------------------------------------------------------------------------
# 模拟安装器环境：MODPATH 指向沙箱模块目录，SYFUSE_DATA_DIR 重定向数据目录。
# ui_print / set_perm_recursive 由安装器提供，这里给最小实现。
INSTALL_LOG="$SANDBOX/install.log"
(
  export MODPATH="$FAKE_MOD"
  export SYFUSE_DATA_DIR="$FAKE_DATA"
  export API=35
  ui_print() { echo "$@"; }
  set_perm_recursive() { :; }
  # shellcheck disable=SC1090
  . "$FAKE_MOD/customize.sh"
) > "$INSTALL_LOG" 2>&1
rc=$?

if [ "$rc" -eq 0 ]; then
  ok "customize.sh 执行成功（退出码 0）"
else
  no "customize.sh 执行失败（退出码 $rc）"
  sed 's/^/      /' "$INSTALL_LOG"
fi

AFTER_INSTALL="$(snapshot "$FAKE_DATA")"
installed_count=$(printf '%s' "$AFTER_INSTALL" | grep -c . || echo 0)
info "安装后 $FAKE_DATA 条目数: $installed_count"
[ "$installed_count" -gt 0 ] && ok "安装确实产生了文件（不是空操作）" || no "安装后数据目录为空，安装逻辑可能没生效"

# ---------------------------------------------------------------------------
head2 "4. 校验安装产物"
# ---------------------------------------------------------------------------
MANIFEST="$FAKE_DATA/manifest/installed.json"
if [ -f "$MANIFEST" ]; then
  ok "还原清单已生成: manifest/installed.json"
  # 清单必须是合法 JSON，且含 restorable=true 的条目
  if python_bin=$(command -v python3 || command -v python 2>/dev/null) && [ -n "${python_bin:-}" ]; then
    if "$python_bin" - "$MANIFEST" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
assert d.get('schemaVersion')==1, 'schemaVersion 应为 1'
assert d.get('moduleId')=='syfuse', 'moduleId 应为 syfuse'
assert d.get('entries'), 'entries 不应为空'
for e in d['entries']:
    assert e.get('restorable') is True, f"条目 {e.get('id')} 标记为不可还原"
    assert e.get('target'), f"条目 {e.get('id')} 缺少 target"
PY
    then
      ok "清单是合法 JSON 且全部条目标记可还原"
    else
      no "清单内容不合规（见上方断言）"
    fi
  else
    info "无 Python，跳过清单 JSON 结构校验"
  fi
else
  no "还原清单未生成 —— 卸载将失去还原依据"
fi

for d in log rules cert backup profiles manifest; do
  [ -d "$FAKE_DATA/$d" ] && ok "目录已创建: $d" || no "目录缺失: $d"
done

if [ -f "$FAKE_DATA/syfuse.conf" ]; then
  ok "默认配置已落位"
  perm=$(stat -c '%a' "$FAKE_DATA/syfuse.conf" 2>/dev/null || echo '?')
  info "syfuse.conf 权限: $perm"
else
  no "默认配置未落位"
fi

# ---------------------------------------------------------------------------
head2 "5. 执行卸载（uninstall.sh）"
# ---------------------------------------------------------------------------
UNINSTALL_LOG="$SANDBOX/uninstall.log"
(
  export SYFUSE_DATA_DIR="$FAKE_DATA"
  sh "$FAKE_MOD/uninstall.sh"
) > "$UNINSTALL_LOG" 2>&1
rc=$?

if [ "$rc" -eq 0 ]; then
  ok "uninstall.sh 退出码 0（自检未发现残留）"
else
  no "uninstall.sh 退出码 $rc（脚本自检报告了残留）"
fi
info "卸载输出："
sed 's/^/      /' "$UNINSTALL_LOG"

# ---------------------------------------------------------------------------
head2 "6. 残留比对（核心断言）"
# ---------------------------------------------------------------------------
AFTER_UNINSTALL="$(snapshot "$FAKE_DATA")"
after_count=$(printf '%s' "$AFTER_UNINSTALL" | grep -c . || echo 0)

if [ -e "$FAKE_DATA" ]; then
  no "数据目录仍然存在: $FAKE_DATA（残留 $after_count 项）"
  printf '%s\n' "$AFTER_UNINSTALL" | sed 's/^/      /'
else
  ok "数据目录已被完全删除"
fi

# 逐项 diff：安装前基线 vs 卸载后
if [ "$BEFORE" = "$AFTER_UNINSTALL" ]; then
  ok "系统状态与安装前完全一致（无残留）"
else
  no "系统状态与安装前不一致"
  printf '      安装前: %s 项\n      卸载后: %s 项\n' \
    "$(printf '%s' "$BEFORE" | grep -c . || echo 0)" "$after_count"
fi

# ---------------------------------------------------------------------------
head2 "7. 令牌与权限（安全相关）"
# ---------------------------------------------------------------------------
# 卸载后令牌文件必须不存在（它是敏感文件）
if [ -e "$FAKE_DATA/api.token" ]; then
  no "令牌文件残留"
else
  ok "令牌文件未残留"
fi

# ---------------------------------------------------------------------------
printf '\n===========================================\n'
printf 'restore-check 汇总: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
if [ "$FAIL" -eq 0 ]; then
  printf '结论：沙箱内安装→卸载完全还原。\n'
  printf '注意：真机上的挂载/证书/iptables 还原**不在**本脚本覆盖范围内，\n'
  printf '      需在真机执行同一流程（见 README 功能可用性报告）。\n'
else
  printf '结论：存在未还原项，不得声称"卸载零残留"。\n'
fi
printf '===========================================\n'
[ "$FAIL" -eq 0 ] || exit 1
exit 0
