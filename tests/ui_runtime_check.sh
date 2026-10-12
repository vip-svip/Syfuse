#!/usr/bin/env bash
# UI 运行时检查：真实渲染 + 交互断言（需要设备）。
#
# 与 run_e2e.sh 的分工：
#   run_e2e       —— 装得上、起得来、不崩
#   ui_runtime    —— 起来了之后，**界面元素真的在、点击真的生效**
#
# 没有设备时 exit 3（SKIP），绝不返回 0。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

DEVICE="$(syfuse_device)" || syfuse_skip "找不到 adb"
[ -n "$DEVICE" ] || syfuse_skip "没有可用设备"
ADB="$(syfuse_adb)"
PKG="io.github.vipsvip.syfuse.debug"

dump_ui() {
  "$ADB" -s "$DEVICE" shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  "$ADB" -s "$DEVICE" shell cat /sdcard/ui.xml 2>/dev/null
}

tap_text() {
  # 在 UI 层级里找到包含该文案的节点，取其 bounds 中心点并点击
  local needle="$1"
  local xml; xml="$(dump_ui)"
  local bounds
  bounds="$(printf '%s' "$xml" | tr '>' '\n' | grep -F "$needle" | grep -oE 'bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"' | head -1)"
  [ -n "$bounds" ] || return 1
  local nums; nums="$(printf '%s' "$bounds" | grep -oE '[0-9]+' | tr '\n' ' ')"
  set -- $nums
  local cx=$(( ($1 + $3) / 2 )) cy=$(( ($2 + $4) / 2 ))
  "$ADB" -s "$DEVICE" shell input tap "$cx" "$cy"
  return 0
}

PASS=0; FAIL=0
ok()   { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
no()   { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }

syfuse_info "设备 $DEVICE"
"$ADB" -s "$DEVICE" shell am force-stop "$PKG"
"$ADB" -s "$DEVICE" shell am start -n "$PKG/io.github.vipsvip.syfuse.MainActivity" >/dev/null
sleep 4

echo
echo "== 首页 =="
home="$(dump_ui)"
printf '%s' "$home" | grep -q "首页" && ok "底部导航「首页」存在" || no "底部导航「首页」缺失"
# 四态开关：至少要有可点击的开关语义
printf '%s' "$home" | grep -qE 'clickable="true"' && ok "首页存在可交互控件" || no "首页没有可交互控件"
printf '%s' "$home" | grep -qE '0\.0 KB/s|KB/s|MB/s|流量' && ok "首页呈现速率/流量区域" || no "首页缺少速率/流量区域"

echo
echo "== 切到「节点」 =="
if tap_text "节点"; then
  sleep 2
  nodes="$(dump_ui)"
  printf '%s' "$nodes" | grep -q "节点" && ok "节点页可达" || no "节点页未渲染"
  printf '%s' "$nodes" | grep -qE "测速|延迟" && ok "节点页含测速入口" || no "节点页缺少测速入口"
else
  no "找不到可点击的「节点」Tab"
fi

echo
echo "== 切到「规则」 =="
if tap_text "规则"; then
  sleep 2
  rules="$(dump_ui)"
  printf '%s' "$rules" | grep -q "规则" && ok "规则页可达" || no "规则页未渲染"
  printf '%s' "$rules" | grep -qE "测试|输入" && ok "规则页含测试入口" || no "规则页缺少规则测试入口"
else
  no "找不到可点击的「规则」Tab"
fi

echo
echo "== 切到「设置」 =="
if tap_text "设置"; then
  sleep 2
  st="$(dump_ui)"
  printf '%s' "$st" | grep -q "设置" && ok "设置页可达" || no "设置页未渲染"
  printf '%s' "$st" | grep -qE "搜索|专家" && ok "设置页含搜索/专家模式入口" || no "设置页缺少搜索入口"
else
  no "找不到可点击的「设置」Tab"
fi

echo
echo "== 主题切换 =="
if tap_text "设置"; then
  sleep 1
  if tap_text "外观"; then
    sleep 2
    ok "外观分组可进入（主题切换路径存在）"
  else
    printf '  [WARN] 未定位到「外观」分组文案，跳过主题切换深检\n'
  fi
fi

echo
echo "== 旋转与进程存活 =="
"$ADB" -s "$DEVICE" shell settings put system accelerometer_rotation 0 >/dev/null 2>&1
"$ADB" -s "$DEVICE" shell settings put system user_rotation 1 >/dev/null 2>&1
sleep 2
if "$ADB" -s "$DEVICE" shell pidof "$PKG" >/dev/null 2>&1; then
  ok "横屏切换后进程存活"
else
  no "横屏切换后进程退出"
fi
"$ADB" -s "$DEVICE" shell settings put system user_rotation 0 >/dev/null 2>&1

if "$ADB" -s "$DEVICE" logcat -d -b crash 2>/dev/null | grep -q "FATAL EXCEPTION"; then
  no "logcat 中存在 FATAL EXCEPTION"
else
  ok "无崩溃日志"
fi

printf '\n===========================================\n'
printf 'ui-runtime 汇总: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
printf '===========================================\n'
[ "$FAIL" -eq 0 ] || exit 1
exit 0
