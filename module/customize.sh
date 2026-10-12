#!/system/bin/sh
# ============================================================================
#  Syfuse 模块安装钩子 (customize.sh)
#  KernelSU / Magisk / APatch 通用。
#
#  注意：本脚本会被安装器 source，$0 指向**安装器本身**，
#        模块目录必须使用安装器提供的 $MODPATH。
#
#  侵入原则（提示词 3.5）：
#   安装过程只写模块目录与 /data/adb/syfuse，**不触碰系统分区**。
#   每一个持久化改动都登记进还原清单，卸载脚本能完整还原。
# ============================================================================
SKIPUNZIP=0

MODDIR="${MODPATH:-${0%/*}}"
DATA_DIR="${SYFUSE_DATA_DIR:-/data/adb/syfuse}"
MANIFEST_DIR="$DATA_DIR/manifest"
MANIFEST="$MANIFEST_DIR/installed.json"

[ -n "$API" ] || API=$(getprop ro.build.version.sdk 2>/dev/null)
[ -n "$API" ] || API=30

ui_print "- Syfuse 模块目录: $MODDIR (Android API $API)"

# ---------- ABI 检测（多重回退，避免单一 getprop 失败导致误判） ----------
ABI=$(getprop ro.product.cpu.abi 2>/dev/null)
[ -z "$ABI" ] && ABI=$(getprop ro.product.cpu.abilist 2>/dev/null | cut -d, -f1)
MACHINE=$(uname -m 2>/dev/null)
ui_print "- 设备架构: ABI=$ABI / machine=$MACHINE"

case "$ABI $MACHINE" in
  *arm64*|*aarch64*)        ARCH=arm64 ;;
  *armeabi*|*armv7*|*arm*)  ARCH=arm ;;
  *x86_64*|*amd64*|*x64*)   ARCH=amd64 ;;
  *)                        ARCH=arm64 ;;
esac
ui_print "- 选用架构: $ARCH"

# ---------- 守护进程二进制（静态随包，R2） ----------
# 二进制随模块包交付，安装时按架构落位；冷启动与首次连接都不下载任何东西。
KERNEL_SRC="$MODDIR/bin/syfused-$ARCH"
if [ -f "$KERNEL_SRC" ]; then
  cp -f "$KERNEL_SRC" "$MODDIR/bin/syfused"
  chmod 0755 "$MODDIR/bin/syfused"
  ui_print "- 已安装守护进程: syfused-$ARCH -> bin/syfused"
else
  ui_print "- ⚠ 未随包提供守护进程二进制（bin/syfused-$ARCH 不存在）"
  ui_print "  本模块包只包含骨架。**这不代表代理可用**，详见 README。"
fi

# ---------- 数据目录 ----------
mkdir -p "$DATA_DIR" "$MANIFEST_DIR" "$DATA_DIR/log" "$DATA_DIR/rules" \
         "$DATA_DIR/cert" "$DATA_DIR/backup" "$DATA_DIR/profiles" 2>/dev/null
chmod 0700 "$DATA_DIR" "$MANIFEST_DIR" "$DATA_DIR/log" "$DATA_DIR/rules" \
           "$DATA_DIR/cert" "$DATA_DIR/backup" "$DATA_DIR/profiles" 2>/dev/null

# ---------- 首次安装落默认配置，不覆盖已有文件 ----------
if [ ! -f "$DATA_DIR/syfuse.conf" ] && [ -f "$MODDIR/config/syfuse.conf" ]; then
  cp -f "$MODDIR/config/syfuse.conf" "$DATA_DIR/syfuse.conf"
  chmod 0600 "$DATA_DIR/syfuse.conf"
  ui_print "- 已写入默认配置: $DATA_DIR/syfuse.conf"
fi

# ---------- 分项配置文件（只补齐缺失的，不覆盖用户已改的） ----------
for f in blacklist.conf whitelist.conf lists.conf sources.conf mitm_rules.conf \
         notify_keywords.conf splash_rules.conf user_rules.txt hook_rules.conf leak_policy.conf; do
  if [ ! -f "$DATA_DIR/rules/$f" ] && [ -f "$MODDIR/config/$f" ]; then
    cp -f "$MODDIR/config/$f" "$DATA_DIR/rules/$f"
  fi
done
chmod 0700 "$DATA_DIR/rules" 2>/dev/null

# ---------- 还原清单：登记本次安装创建的持久化改动 ----------
# 清单格式与 src/internal/manifest 的 Go 结构一致（schemaVersion=1）。
#
# 这里用 shell 写 JSON 而不调用 syfused：安装阶段二进制可能尚未就位
# （上面的分支已经允许它缺失），而清单**必须无条件写成功** ——
# 清单写不出来就等于没有还原依据，那是不可接受的。
#
# 路径必须做 JSON 转义：Android 上是 /data/adb/syfuse 不含特殊字符，
# 但自动化测试会把 DATA_DIR 重定向到带反斜杠的 Windows 路径，
# 不转义就会写出非法 JSON（实测踩到过：Invalid \escape）。
# 反斜杠必须先转，否则会被二次转义。
json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

if [ ! -f "$MANIFEST" ]; then
  NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")
  VER=$(grep '^version=' "$MODDIR/module.prop" 2>/dev/null | cut -d= -f2 | sed 's/^v//')
  ESC_DATA=$(json_escape "$DATA_DIR")
  ESC_MOD=$(json_escape "$MODDIR")
  cat > "$MANIFEST" <<EOF
{
  "schemaVersion": 1,
  "moduleId": "syfuse",
  "moduleVersion": "$VER",
  "installedAt": "$NOW",
  "updatedAt": "$NOW",
  "entries": [
    {
      "id": "datadir",
      "kind": "dir",
      "target": "$ESC_DATA",
      "source": "customize.sh",
      "restorable": true,
      "createdAt": "$NOW",
      "note": "Syfuse 数据根目录，卸载时整体删除"
    },
    {
      "id": "module-dir",
      "kind": "module",
      "target": "$ESC_MOD",
      "source": "安装器",
      "restorable": true,
      "createdAt": "$NOW",
      "note": "模块目录，由模块管理器删除"
    }
  ]
}
EOF
  chmod 0600 "$MANIFEST"
  ui_print "- 已创建还原清单: $MANIFEST"
else
  ui_print "- 还原清单已存在，保留不覆盖（以免丢失既有还原依据）"
fi

# ---------- 权限收敛 ----------
set_perm_recursive "$MODDIR" 0 0 0755 0644 2>/dev/null || true
[ -f "$MODDIR/bin/syfused" ] && chmod 0755 "$MODDIR/bin/syfused"
for s in "$MODDIR"/*.sh; do
  [ -f "$s" ] && chmod 0755 "$s"
done

ui_print "- 安装完成。"
ui_print "  提示：本版本内核与 APK 均未随包交付，代理功能不可用。"
ui_print "  运行 action.sh（模块管理器的「动作」按钮）可查看逐项状态与自检。"
