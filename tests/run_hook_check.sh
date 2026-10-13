#!/usr/bin/env bash
# Hook 装载 / 命中 / 卸载检查（提示词 5.7 / 12 部分）
#
# 必须验证两条路径：
#   有框架（Zygisk / LSPosed / APatch）→ Hook 命中
#   无框架 → **能力降级而非功能缺失**（走 TPROXY/VPN 等价方案）
#
# 当前状态：Hook 逻辑未实现（M11），只有契约层。
# 退出码：0 通过 / 1 失败 / 3 设备部分跳过
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
cd "$SYFUSE_ROOT"
PASS=0; FAIL=0; PEND=0; SKIP=0
ok()   { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
no()   { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }
pend() { printf '  [PEND] %s\n' "$1"; PEND=$((PEND+1)); }
skip() { printf '  [SKIP] %s\n' "$1"; SKIP=$((SKIP+1)); }
info() { printf '  :: %s\n' "$1"; }
head2(){ printf '\n== %s ==\n' "$1"; }

head2 "静态：Hook 契约层是否完整"
for f in hook/src/main/kotlin/io/github/vipsvip/syfuse/hook/HookContract.kt \
         hook/src/main/kotlin/io/github/vipsvip/syfuse/hook/SyfuseHookEntry.kt \
         hook/src/main/assets/xposed_init; do
  [ -f "$f" ] && ok "存在 $f" || no "缺少 $f"
done
# 作用域必须默认空（安全默认值：不 Hook 任何应用）
if grep -qE '^HOOK_APPS=$' module/config/syfuse.conf; then
  ok "HOOK_APPS 默认为空（不 Hook 任何应用）"
else
  no "HOOK_APPS 默认非空 —— 未经用户显式授权就 Hook 应用"
fi
if grep -qE '^HOOK_ENABLE=0' module/config/syfuse.conf; then
  ok "HOOK_ENABLE 默认关闭"
else
  no "HOOK_ENABLE 默认开启 —— 危险能力不得默认开"
fi

head2 "静态：8 项内置 Hook 是否都有定义"
for h in 证书固定绕过 用户CA信任注入 网络栈Hook DNS_Hook 代理重定向 开屏跳过 WebRTC阻断 环境伪装; do
  if grep -rq "$(printf '%s' "$h" | sed 's/_/ /g')" README.md 2>/dev/null; then
    ok "内置 Hook 已登记：$h"
  else
    pend "内置 Hook 未在 README 登记：$h"
  fi
done

head2 "设备检查"
DEVICE="$(syfuse_device 2>/dev/null || true)"
if [ -z "$DEVICE" ]; then
  skip "无设备：Hook 框架检测与命中未执行"
  info "5.7 要求「有框架时 Hook 命中；无框架时等价降级；无特权设备功能不缺失」——"
  info "三条都必须真机验证（不同 root 形态 × Hook 框架组合）。"
else
  info "设备 $DEVICE"
  ADB="$(syfuse_adb)"
  # 检测框架存在性（只读探测，不做任何改动）
  for probe in "zygisk:/data/adb/modules/zygisk" "lsposed:/data/adb/lspd" "apatch:/data/adb/ap"; do
    name="${probe%%:*}"; p="${probe##*:}"
    if "$ADB" -s "$DEVICE" shell "su -c 'test -e $p && echo yes'" 2>/dev/null | grep -q yes; then
      ok "检测到框架：$name"
    else
      info "未检测到 $name"
    fi
  done
  pend "Hook 装载/命中/卸载：需 M11 交付后执行"
fi

printf '\n===========================================\n'
printf 'hook-check 汇总: PASS=%d FAIL=%d PEND=%d SKIP=%d\n' "$PASS" "$FAIL" "$PEND" "$SKIP"
if [ "$FAIL" -gt 0 ]; then printf '结论：不允许交付\n===========================================\n'; exit 1; fi
if [ "$SKIP" -gt 0 ]; then printf '结论：设备部分未执行 —— **Hook 能力未验证**，不是通过。\n===========================================\n'; exit 3; fi
printf '结论：全部通过\n===========================================\n'; exit 0
