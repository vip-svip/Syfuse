#!/system/bin/sh
#
# Syfuse 模块安装脚本（KernelSU / Magisk 通用）。
#
# 设计约束（提示词 6.9 侵入原则）：
#  1. 所有系统级改动走 OverlayFS 或 bind mount，不直接写系统分区
#  2. 每项改动登记到 manifest/installed.json
#  3. uninstall.sh 必须能完整还原
#
# M1 状态：本脚本只建立骨架与目录，不执行任何实际改动。
#         实际挂载逻辑在 M11 接入。

SKIPUNZIP=0

MODDIR=${0%/*}

ui_print "- Syfuse 模块安装"
ui_print "- 目标模块 id: syfuse"

# ---------------------------------------------------------------------------
# 环境检测：KernelSU 与 Magisk 的差异处理
# ---------------------------------------------------------------------------
if [ -n "$KSU" ]; then
    ui_print "- 检测到 KernelSU (版本: ${KSU_VER:-unknown})"
    ROOT_PROVIDER="kernelsu"
elif [ -n "$MAGISK_VER" ]; then
    ui_print "- 检测到 Magisk (版本: ${MAGISK_VER})"
    ROOT_PROVIDER="magisk"
else
    ui_print "- 未检测到已知 root 提供方，模块将以受限模式运行"
    ROOT_PROVIDER="unknown"
fi

# ---------------------------------------------------------------------------
# ABI 检测：决定随包交付哪个内核
# ---------------------------------------------------------------------------
ARCH=$(getprop ro.product.cpu.abi)
ui_print "- 设备 ABI: ${ARCH}"

case "${ARCH}" in
    arm64-v8a)   KERNEL_ABI="arm64" ;;
    armeabi-v7a) KERNEL_ABI="arm" ;;
    x86_64)      KERNEL_ABI="x86_64" ;;
    *)
        ui_print "! 不支持的 ABI: ${ARCH}"
        KERNEL_ABI=""
        ;;
esac

# ---------------------------------------------------------------------------
# 建立工作目录
#
# 全部数据放在 /data/adb/syfuse 下，不碰系统分区。
# 这是「卸载即完全复原」的基础：只删这一个目录即可。
# ---------------------------------------------------------------------------
SYFUSE_DATA="/data/adb/syfuse"

mkdir -p "${SYFUSE_DATA}/bin"
mkdir -p "${SYFUSE_DATA}/manifest"
mkdir -p "${SYFUSE_DATA}/rules"
mkdir -p "${SYFUSE_DATA}/logs"

# 改动清单初始化（提示词 6.9 要求）。
# 用 {} 空对象而非空文件：JSON 解析器对空文件会报错，
# 而空对象是合法 JSON，下游读取更健壮。
if [ ! -f "${SYFUSE_DATA}/manifest/installed.json" ]; then
    echo '{"entries":[]}' > "${SYFUSE_DATA}/manifest/installed.json"
fi

# 记录安装信息供卸载时参考
cat > "${SYFUSE_DATA}/install.info" <<EOF
root_provider=${ROOT_PROVIDER}
kernel_abi=${KERNEL_ABI}
module_dir=${MODDIR}
installed_at=$(date +%s)
EOF

# ---------------------------------------------------------------------------
# 权限设置
# ---------------------------------------------------------------------------
set_perm_recursive "${SYFUSE_DATA}" 0 0 0755 0644

ui_print "- 安装完成"
ui_print "- 数据目录: ${SYFUSE_DATA}"
ui_print "- 卸载时该目录与所有挂载将一并清理"
