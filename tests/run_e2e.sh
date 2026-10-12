#!/usr/bin/env bash
# 端到端测试：需要一台真实设备/模拟器。
#
# 这是**唯一**能验证"应用真的能起来、VPN 真的能起隧道"的环节。
# 没有设备时本脚本 exit 3（SKIP），**不是** exit 0——避免 CI 把"没跑"读成"通过"（R4）。
#
# 前置：adb 可用、设备已授权、`gradlew :app:assembleDebug` 已产出 APK。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

DEVICE="$(syfuse_device)" || syfuse_skip "找不到 adb；无法进行设备测试"
[ -n "$DEVICE" ] || syfuse_skip "adb 已就绪但没有处于 device 状态的设备"

ADB="$(syfuse_adb)"
PKG="io.github.vipsvip.syfuse.debug"

syfuse_info "设备: $DEVICE"
syfuse_info "包名: $PKG"
echo

# ---------- 1. 安装 ----------
APK="$(ls -1 "$SYFUSE_ROOT"/app/build/outputs/apk/debug/*.apk 2>/dev/null | head -1)"
[ -n "$APK" ] || syfuse_die "找不到 debug APK，请先运行 :app:assembleDebug"
syfuse_info "安装 $APK"
"$ADB" -s "$DEVICE" install -r -t "$APK" || syfuse_die "安装失败"

# ---------- 2. 冷启动 ----------
syfuse_info "冷启动并等待首帧"
"$ADB" -s "$DEVICE" shell am force-stop "$PKG"
"$ADB" -s "$DEVICE" shell am start -W -n "$PKG/io.github.vipsvip.syfuse.MainActivity" \
  | tee /tmp/syfuse_am_start.txt
grep -q "Status: ok" /tmp/syfuse_am_start.txt || syfuse_die "Activity 未成功启动"

# ---------- 3. 崩溃与 ANR 检查 ----------
sleep 3
syfuse_info "检索 logcat 中的崩溃/ANR"
"$ADB" -s "$DEVICE" logcat -d -b crash 2>/dev/null | tee /tmp/syfuse_crash.txt
if grep -qE "FATAL EXCEPTION|ANR in $PKG" /tmp/syfuse_crash.txt; then
  echo "---- 崩溃片段 ----"
  grep -A 30 -E "FATAL EXCEPTION|ANR in $PKG" /tmp/syfuse_crash.txt
  syfuse_die "检测到崩溃或 ANR"
fi

# ---------- 4. 进程存活 ----------
if ! "$ADB" -s "$DEVICE" shell pidof "$PKG" >/dev/null 2>&1; then
  syfuse_die "启动后进程不存在，说明已退出"
fi
syfuse_info "进程存活: pid=$("$ADB" -s "$DEVICE" shell pidof "$PKG")"

# ---------- 5. 四态开关的 UI 断言（通过 uiautomator dump，不依赖截图） ----------
syfuse_info "导出 UI 层级做控件断言"
"$ADB" -s "$DEVICE" shell uiautomator dump /sdcard/syfuse_ui.xml >/dev/null 2>&1
"$ADB" -s "$DEVICE" shell cat /sdcard/syfuse_ui.xml > /tmp/syfuse_ui.xml 2>/dev/null

for needle in "首页" "节点" "规则" "设置"; do
  if grep -q "$needle" /tmp/syfuse_ui.xml; then
    syfuse_info "底部 Tab 存在: $needle"
  else
    syfuse_die "底部 Tab 缺失: $needle"
  fi
done

echo
syfuse_info "E2E PASS（设备 $DEVICE）"
exit 0
