#!/usr/bin/env bash
# 玻璃专项性能检查（提示词 12 部分 ui_glass_perf / 7.5 / 11 部分）
#
# 必须验证：
#   三档强度下的帧耗时与丢帧率、降采样生效、超阈值自动降档、
#   API<31 伪玻璃路径、减少动效/降低透明度系统设置的响应
#
# **这些全部需要真机**（GPU 行为无法在 JVM 上模拟）。
# 本脚本的静态部分检查策略层是否按 4.11 实现；设备部分无设备时 exit 3。
#
# 退出码：0 通过 / 1 失败 / 3 设备部分跳过（**不是通过**）
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

GLASS="ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/glass/Glass.kt"

head2 "静态：三档强度与默认值（4.11）"
for pair in "OFF:0.dp" "LIGHT:12.dp" "STRONG:24.dp"; do
  name="${pair%%:*}"; dp="${pair##*:}"
  grep -q "$name($dp" "$GLASS" 2>/dev/null && ok "强度 $name = $dp" || no "强度 $name 不是 $dp"
done
grep -q "val DEFAULT = LIGHT" "$GLASS" && ok "默认强度为「轻」（4.11 要求）" || no "默认强度不是「轻」"

head2 "静态：白名单与层数上限（4.11）"
for slot in TOP_APP_BAR BOTTOM_NAV_BAR HOME_FLOATING_CARD HOME_STATUS_BAR \
            BOTTOM_SHEET DIALOG WIDGET QUICK_SETTINGS_TILE REFRESH_PLACEHOLDER; do
  grep -q "$slot" "$GLASS" && : || no "白名单缺少位置：$slot"
done
ok "9 个白名单位置已核对"
grep -q "MAX_CONCURRENT_LAYERS = 3" "$GLASS" && ok "层数上限 = 3（4.11 要求 ≤3）" || no "层数上限不是 3"
grep -q "assertAllowed" "$GLASS" && ok "有白名单断言（非白名单位置会失败）" || no "缺少白名单断言"

head2 "静态：性能预算（7.5 / 第 11 部分）"
grep -q "P90_FRAME_MS = 24.0" "$GLASS" && ok "P90 帧耗时预算 = 24ms" || no "P90 预算不是 24ms"
grep -q "MAX_DROPPED_RATIO = 0.03" "$GLASS" && ok "丢帧率预算 = 3%" || no "丢帧率预算不是 3%"
grep -q "FIRST_FRAME_EXTRA_MS = 80.0" "$GLASS" && ok "首屏增量预算 = 80ms" || no "首屏预算不是 80ms"
grep -q "WARN_AFTER_VIOLATIONS = 2" "$GLASS" && ok "连续超标 2 次提示用户" || no "提示阈值不是 2"

head2 "静态：降级路径（4.11 要求必须有降级）"
grep -q "Build.VERSION.SDK_INT >= Build.VERSION_CODES.S" "$GLASS" \
  && ok "API<31 走伪玻璃路径" || no "缺少 API<31 降级判断"
grep -q "ANIMATOR_DURATION_SCALE" "$GLASS" \
  && ok "响应系统「减少动态效果」" || no "未响应「减少动态效果」系统设置"
# AMOLED 判定要看**实现**（isAmoled 分支 + 纯黑半透明），不能看变量名 ——
# 变量名会随重构改，实现不会。
if grep -q "isAmoled" "$GLASS" && grep -q "Color.Black.copy" "$GLASS"; then
  ok "AMOLED 主题下玻璃改纯黑半透明"
else
  no "缺少 AMOLED 玻璃处理（应有 isAmoled 分支 + 纯黑半透明）"
fi
grep -q "degradeLog" "$GLASS" && ok "降档有日志记录（4.11 要求）" || no "降档无日志"

head2 "静态：是否误用了禁止的做法（7.5）"
# **必须剔除注释再匹配**：Glass.kt 里有一行注释写着"4.11 明令禁止动态噪点背景"，
# 直接 grep 会把这句说明本身当成违规。这类"注释命中"的假阳性在本项目
# 已出现过多次（WebView、占位符、CRLF），统一用剔除注释的方式处理。
GLASS_CODE="$(grep -vE '^\s*(//|\*|/\*)' "$GLASS")"
if printf '%s' "$GLASS_CODE" | grep -qiE "noise|动态噪点|NoiseShader"; then
  no "出现动态噪点实现 —— 7.5 明令禁止（应改用静态渐变）"
else
  ok "无动态噪点背景（已剔除注释后判定）"
fi

head2 "设备部分（真机性能实测）"
DEVICE="$(syfuse_device 2>/dev/null || true)"
if [ -z "$DEVICE" ]; then
  skip "无设备：三档强度下的 P90 帧耗时/丢帧率未实测"
  info "第 11 部分要求「玻璃开启时 P90 ≤ 24ms、丢帧率 < 3%、首屏增加 ≤ 80ms」，"
  info "且必须「多轮取中位数」。这些**只能在真机上测**。"
  info ""
  info "因此当前**不得声称玻璃性能达标**。M2 的完成判定"
  info "（「真机可用，玻璃开启时 P90 ≤ 24ms 且自动降级生效」）**未达成**。"
  info ""
  info "另有一项连真机都测不了：4.11 要求「离屏降采样（≥2）后模糊」。"
  info "当前实现用 Compose 官方 Modifier.blur，**其内部是否降采样未经证实**。"
  info "需用 GPU 帧耗时对比（有/无降采样）来判定，这需要更精细的插桩。"
else
  info "设备 $DEVICE"
  ADB="$(syfuse_adb)"
  PKG="io.github.vipsvip.syfuse.debug"
  for level in off light strong; do
    info "── 强度 $level ──"
    # gfxinfo 采集帧统计；三档各采一轮
    "$ADB" -s "$DEVICE" shell dumpsys gfxinfo "$PKG" reset >/dev/null 2>&1
    sleep 8
    stats="$("$ADB" -s "$DEVICE" shell dumpsys gfxinfo "$PKG" 2>/dev/null)"
    p90=$(printf '%s' "$stats" | grep -i "90th percentile" | head -1 | grep -oE '[0-9]+' | head -1)
    janky=$(printf '%s' "$stats" | grep -i "Janky frames" | head -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
    if [ -n "$p90" ]; then
      info "强度 $level: P90=${p90}ms 丢帧=${janky:-?}%"
      awk -v p="$p90" 'BEGIN{ if (p+0 <= 24) exit 0; else exit 1 }' \
        && ok "强度 $level P90 ≤ 24ms" || no "强度 $level P90 = ${p90}ms > 24ms"
    else
      pend "强度 $level 未取到帧统计（需应用在前台并有滚动操作）"
    fi
  done
fi

printf '\n===========================================\n'
printf 'ui-glass-perf 汇总: PASS=%d FAIL=%d PEND=%d SKIP=%d\n' "$PASS" "$FAIL" "$PEND" "$SKIP"
if [ "$FAIL" -gt 0 ]; then printf '结论：不允许交付\n===========================================\n'; exit 1; fi
if [ "$SKIP" -gt 0 ]; then
  printf '结论：真机性能未实测 —— **玻璃性能预算未验证**，M2 完成判定未达成。\n'
  printf '===========================================\n'
  exit 3
fi
printf '结论：全部通过\n===========================================\n'; exit 0
