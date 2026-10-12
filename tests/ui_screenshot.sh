#!/usr/bin/env bash
# 截图采集：为每个主页面 + 每个主题各出一张图，存到 dist/screenshots/。
#
# 用途：让"界面像不像 Shadowrocket"这件事可以被**看见**，而不是靠文字描述。
# 没有设备时 exit 3（SKIP）。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

DEVICE="$(syfuse_device)" || syfuse_skip "找不到 adb"
[ -n "$DEVICE" ] || syfuse_skip "没有可用设备"
ADB="$(syfuse_adb)"
PKG="io.github.vipsvip.syfuse.debug"

OUT="$SYFUSE_ROOT/dist/screenshots"
mkdir -p "$OUT"

tap_text() {
  local needle="$1"
  "$ADB" -s "$DEVICE" shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  local xml; xml="$("$ADB" -s "$DEVICE" shell cat /sdcard/ui.xml 2>/dev/null)"
  local bounds
  bounds="$(printf '%s' "$xml" | tr '>' '\n' | grep -F "$needle" | grep -oE 'bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"' | head -1)"
  [ -n "$bounds" ] || return 1
  local nums; nums="$(printf '%s' "$bounds" | grep -oE '[0-9]+' | tr '\n' ' ')"
  set -- $nums
  "$ADB" -s "$DEVICE" shell input tap "$(( ($1 + $3) / 2 ))" "$(( ($2 + $4) / 2 ))"
}

shot() {
  local name="$1"
  local file="$OUT/$name.png"
  "$ADB" -s "$DEVICE" exec-out screencap -p > "$file" 2>/dev/null
  if [ -s "$file" ]; then
    syfuse_info "已保存 $file ($(stat -c '%s' "$file" 2>/dev/null || stat -f '%z' "$file") 字节)"
  else
    echo "  截图失败: $name" >&2
  fi
}

syfuse_info "设备 $DEVICE，输出 $OUT"
"$ADB" -s "$DEVICE" shell am force-stop "$PKG"
"$ADB" -s "$DEVICE" shell am start -n "$PKG/io.github.vipsvip.syfuse.MainActivity" >/dev/null
sleep 4

shot "01_home"
tap_text "节点" && { sleep 2; shot "02_nodes"; }
tap_text "规则" && { sleep 2; shot "03_rules"; }
tap_text "设置" && { sleep 2; shot "04_settings"; }

# 主题轮换：进入外观分组后依次切换，每个主题一张。
# 主题文案取自 SettingsCatalog 的 theme 选项；找不到就如实跳过，不伪造图片。
if tap_text "设置" && tap_text "外观"; then
  sleep 2
  shot "05_settings_appearance"
  i=0
  for theme in "浅色" "深色" "跟随系统" "纯黑"; do
    if tap_text "$theme"; then
      i=$((i+1)); sleep 2; shot "06_theme_${i}_${theme}"
    fi
  done
  [ "$i" -eq 0 ] && printf '  [WARN] 未定位到任何主题选项文案，未产出主题截图\n'
else
  printf '  [WARN] 未进入外观分组，未产出主题截图\n'
fi

echo
syfuse_info "产出清单："
ls -1 "$OUT" 2>/dev/null | sed 's/^/  /'
syfuse_info "截图数量: $(ls -1 "$OUT" 2>/dev/null | wc -l | tr -d ' ')"
exit 0
