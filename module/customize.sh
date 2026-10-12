#!/system/bin/sh
# ============================================================
#  Syfuse 模块安装钩子 (customize.sh)
#  KernelSU / Magisk 通用。
#
#  注意：本脚本会被安装器 source，$0 指向安装器本身，
#        模块目录必须使用标准变量 $MODPATH。
#
#  侵入原则（6.9）：安装过程只写模块目录与 /data/adb/syfuse，
#  不触碰系统分区文件。卸载脚本能完整还原。
# ============================================================
SKIPUNZIP=0

if [ -n "$MODPATH" ]; then
  MODDIR="$MODPATH"
else
  MODDIR="${0%/*}"
fi

DATA_DIR=/data/adb/syfuse

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
  *x86_64*|*amd64*|*x64*)   ARCH=x86_64 ;;
  *i386*|*i686*|*x86*)      ARCH=x86 ;;
  *)                        ARCH=arm64 ;;
esac
ui_print "- 选用架构: $ARCH"

# ---------- 安装内核二进制（若随包提供） ----------
# 内核静态内置（R2）：二进制随模块包交付，安装时按架构落位，
# 冷启动与首次连接都不下载任何东西。
KERNEL_SRC="$MODDIR/bin/syfuse-$ARCH"
if [ -f "$KERNEL_SRC" ]; then
  mkdir -p "$MODDIR/bin"
  cp -f "$KERNEL_SRC" "$MODDIR/bin/syfuse"
  chmod 0755 "$MODDIR/bin/syfuse"
  ui_print "- 已安装内核: syfuse-$ARCH -> bin/syfuse"
else
  ui_print "- 未随包提供内核二进制（本版本尚未集成内核，模块仅提供特权层骨架）"
fi

# ---------- 数据目录 ----------
mkdir -p "$DATA_DIR"
chmod 0700 "$DATA_DIR"

# 首次安装时落一份默认配置，不覆盖已有文件
if [ ! -f "$DATA_DIR/syfuse.conf" ] && [ -f "$MODDIR/config/syfuse.conf" ]; then
  cp -f "$MODDIR/config/syfuse.conf" "$DATA_DIR/syfuse.conf"
  ui_print "- 已写入默认配置到 $DATA_DIR/syfuse.conf"
fi

# ---------- 权限收敛 ----------
set_perm_recursive "$MODDIR" 0 0 0755 0644 2>/dev/null || true
[ -f "$MODDIR/bin/syfuse" ] && chmod 0755 "$MODDIR/bin/syfuse"

ui_print "- 安装完成。卸载请在模块管理器中移除，或运行 module/uninstall.sh。"
